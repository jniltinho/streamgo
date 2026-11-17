package output

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/core/stage"
)

// TickEvery is how often the timers (start timeout, deadline, retries) are checked.
const TickEvery = 250 * time.Millisecond

// UpstreamTimeout bounds each call to the media server made outside a request.
const UpstreamTimeout = 10 * time.Second

// Conflict is a request the output's state refuses, with a message safe to show (why: live
// broadcast, destination in use, server at its limit). It matches core.ErrConflict.
type Conflict struct{ Message string }

func (c *Conflict) Error() string { return c.Message }

// Is makes errors.Is(err, core.ErrConflict) true.
func (c *Conflict) Is(target error) bool { return target == core.ErrConflict }

func conflict(format string, args ...any) error {
	return &Conflict{Message: fmt.Sprintf(format, args...)}
}

// Notify receives a process's events: flowing once when its output's media time first
// advances, then, exactly once, ended (flowing false) with the reason, already redacted.
type Notify func(flowing bool, reason string)

// Stopper stops a process: SIGINT, then SIGKILL after a grace. It does not wait.
type Stopper interface{ Stop() }

// Processes runs the transcoder and the relays (internal/ffmpeg implements it).
type Processes interface {
	// StartTranscoder encodes the broadcast's program at the profile's size and bitrate.
	StartTranscoder(broadcastID string, p broadcast.Profile, notify Notify) Stopper
	StartRelay(broadcastID string, target destination.Target, notify Notify) Stopper
	// StopAll stops every process at once under one deadline and waits for them (shutdown).
	StopAll()
}

// Answer is the media server's answer to a WHIP offer.
type Answer struct {
	SDP      []byte
	Resource string   // the upstream session resource's absolute URL
	ETag     string   // may be ""
	Links    []string // Link headers (ICE servers)
}

// ErrUpstream is the media server refusing or not answering; the HTTP layer answers 502.
var ErrUpstream = errors.New("the media server did not accept the program")

// Ingest is the media server (internal/mediamtx implements it). Publish answers an error
// wrapping ErrUpstream when the server refuses or does not answer.
type Ingest interface {
	Publish(ctx context.Context, broadcastID string, offer []byte) (Answer, error)
	// Close deletes an upstream session resource; one already gone is not an error.
	Close(ctx context.Context, resource string) error
	// Publisher is the session publishing on the broadcast's program path, "" when none.
	Publisher(ctx context.Context, broadcastID string) (string, error)
	// Publishing maps every broadcast with a session publishing its program to that session.
	Publishing(ctx context.Context) (map[string]string, error)
	Kick(ctx context.Context, session string) error
}

// Destinations reads destinations and the broadcasts' selections (destination.Repository).
type Destinations interface {
	Get(ctx context.Context, id string) (*destination.Destination, error)
	ListByBroadcast(ctx context.Context, broadcastID string) ([]destination.Destination, error)
}

// Targets gives a relay's target, checking its host again (destination.Service).
type Targets interface {
	Target(ctx context.Context, id string) (destination.Target, error)
}

// OnAir turns the stage's on_air on and off for a caller holding the lock (stage.Writer).
type OnAir interface {
	SetOnAir(ctx context.Context, id string, on bool) (stage.State, error)
}

// Service is the live output. Every field is required except Quality, Logger and Now. It implements
// destination.Statuses and destination.SelectionNotifier.
type Service struct {
	Broadcasts    broadcast.Repository
	Locks         *broadcast.Locks
	Destinations  Destinations
	Targets       Targets
	Stage         OnAir
	Ingest        Ingest
	Processes     Processes
	MaxConcurrent int           // output.max_concurrent
	StartTimeout  time.Duration // output.start_timeout
	Grace         time.Duration // output.reconnect_grace
	// Quality gives the transcoder the broadcast's profile (nil or zero: 720p). It is read at a run's first
	// transcoder; the run keeps that profile.
	Quality func() broadcast.Quality
	Logger  *slog.Logger
	Now     func() time.Time

	// mu is the global lock: capacity, destination claims and the in-memory states. It is
	// taken after a broadcast's lock, never before.
	mu      sync.Mutex
	states  map[string]State              // broadcasts not offline; values are never modified
	claims  map[string]string             // destination id → broadcast id
	procs   map[string]map[string]Stopper // broadcast id → "" (transcoder) or destination id → process
	closing bool
}

var (
	_ destination.Statuses          = (*Service)(nil)
	_ destination.SelectionNotifier = (*Service)(nil)
)

// GoLive moves the owner's offline broadcast to starting and starts waiting for its program.
func (s *Service) GoLive(ctx context.Context, owner, id string) (broadcast.Output, error) {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return broadcast.Output{}, err
	}
	defer unlock()
	b, err := s.owned(ctx, owner, id)
	if err != nil {
		return broadcast.Output{}, err
	}
	if b.Output.State != broadcast.Offline {
		return broadcast.Output{}, conflict("The broadcast is already %s.", b.Output.State)
	}
	dests, err := s.Destinations.ListByBroadcast(ctx, id)
	if err != nil {
		return broadcast.Output{}, err
	}
	if len(dests) == 0 {
		return broadcast.Output{}, conflict("Choose at least one destination before going live.")
	}
	if err := s.reserve(id, dests); err != nil {
		return broadcast.Output{}, err
	}
	ids := make([]string, len(dests))
	for i, d := range dests {
		ids[i] = d.ID
	}
	if _, err := s.apply(ctx, b, Event{Kind: GoLive, Dests: ids}); err != nil {
		s.release(id)
		return broadcast.Output{}, err
	}
	return b.Output, nil
}

// End ends the owner's broadcast from any state; an offline broadcast is answered as it is.
// It returns once the processes were told to stop and the ingest session is closed.
func (s *Service) End(ctx context.Context, owner, id string) (broadcast.Output, error) {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return broadcast.Output{}, err
	}
	b, err := s.owned(ctx, owner, id)
	if err != nil || b.Output.State == broadcast.Offline {
		unlock()
		if err != nil {
			return broadcast.Output{}, err
		}
		return b.Output, nil
	}
	after, err := s.apply(ctx, b, Event{Kind: End})
	unlock()
	after(context.WithoutCancel(ctx))
	return b.Output, err
}

// StatusOf reports the relay status of the broadcast's destinations (destination.Statuses).
func (s *Service) StatusOf(broadcastID string) map[string]destination.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.states[broadcastID]
	if !ok {
		return nil
	}
	out := make(map[string]destination.Status, len(st.Dests))
	for id, d := range st.Dests {
		out[id] = destination.Status{State: d.Status, Error: d.Error}
	}
	return out
}

// SelectionChanged starts the relays of added destinations and stops those of removed ones
// for a broadcast that is not offline (destination.SelectionNotifier); the caller holds the
// broadcast's lock. It refuses removing the last destination and adding one that another
// active broadcast uses.
func (s *Service) SelectionChanged(ctx context.Context, broadcastID string, added, removed []string) error {
	b, err := s.Broadcasts.Get(ctx, broadcastID)
	if err != nil {
		return err
	}
	if b.Output.State == broadcast.Offline {
		return nil
	}
	current := s.state(broadcastID).Dests
	left := len(current) + len(added)
	for _, id := range removed {
		if _, ok := current[id]; ok {
			left--
		}
	}
	if left <= 0 {
		return conflict("A live broadcast needs at least one destination; end the broadcast to remove the last one.")
	}
	s.mu.Lock()
	for _, id := range added {
		if other := s.claims[id]; other != "" && other != broadcastID {
			s.mu.Unlock()
			return s.inUse(ctx, id)
		}
	}
	for _, id := range added {
		s.claims[id] = broadcastID
	}
	for _, id := range removed {
		if s.claims[id] == broadcastID {
			delete(s.claims, id)
		}
	}
	s.mu.Unlock()
	after, err := s.apply(ctx, b, Event{Kind: DestinationsChanged, Run: b.Output.Run, Added: added, Removed: removed})
	go after(context.WithoutCancel(ctx))
	return err
}

// Run recovers the broadcasts left live by the previous process, then checks the timers every
// TickEvery until ctx ends, and then stops every process, leaving the stored states as they are.
func (s *Service) Run(ctx context.Context) {
	if err := s.recoverAll(ctx); err != nil && ctx.Err() == nil {
		s.logger().Error("recover the live broadcasts", "error", err)
	}
	t := time.NewTicker(TickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.closing = true
			s.mu.Unlock()
			s.Processes.StopAll()
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// CloseOrphans closes every ingest session publishing for a broadcast that is offline or gone
// (a reconciler step, design D3).
func (s *Service) CloseOrphans(ctx context.Context) error {
	sessions, err := s.Ingest.Publishing(ctx)
	if err != nil {
		return fmt.Errorf("list the ingest sessions: %w", err)
	}
	var errs []error
	for _, id := range slices.Sorted(maps.Keys(sessions)) {
		if err := s.kickIfOffline(ctx, id, sessions[id]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Tick checks the timers of every broadcast that is not offline once; Run calls it every
// TickEvery.
func (s *Service) Tick(ctx context.Context) {
	s.mu.Lock()
	ids := slices.Sorted(maps.Keys(s.states))
	s.mu.Unlock()
	for _, id := range ids {
		s.dispatch(ctx, id, Event{Kind: Tick}, true)
	}
}

func (s *Service) recoverAll(ctx context.Context) error {
	list, err := s.Broadcasts.ListByOutputState(ctx, broadcast.Starting, broadcast.Live, broadcast.Interrupted)
	if err != nil {
		return err
	}
	var errs []error
	for _, b := range list {
		if err := s.recover(ctx, b.ID); err != nil {
			errs = append(errs, fmt.Errorf("broadcast %s: %w", b.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) recover(ctx context.Context, id string) error {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	b, err := s.Broadcasts.Get(ctx, id)
	if err != nil || b.Output.State == broadcast.Offline {
		return err
	}
	dests, err := s.Destinations.ListByBroadcast(ctx, id)
	if err != nil {
		return err
	}
	ids := make([]string, len(dests))
	s.mu.Lock()
	s.init()
	s.states[id] = State{Output: b.Output}
	for i, d := range dests {
		ids[i] = d.ID
		s.claims[d.ID] = id
	}
	s.mu.Unlock()
	_, err = s.apply(ctx, b, Event{Kind: Recover, Dests: ids})
	return err
}

// dispatch applies e to broadcast id under its lock. Upstream calls that follow run after the
// lock is released, in the background when async.
func (s *Service) dispatch(ctx context.Context, id string, e Event, async bool) {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return
	}
	if s.isClosing() {
		unlock()
		return
	}
	b, err := s.Broadcasts.Get(ctx, id)
	if err != nil {
		unlock()
		s.logger().Warn("live output event", "broadcast", id, "error", err)
		return
	}
	after, err := s.apply(ctx, b, e)
	unlock()
	if err != nil {
		s.logger().Warn("live output event", "broadcast", id, "error", err)
	}
	if async {
		go after(context.WithoutCancel(ctx))
	} else {
		after(ctx)
	}
}

// apply runs the machine for broadcast b, whose lock the caller holds: it stores the new
// output, carries out the commands and keeps the new state; b.Output is updated. The returned
// function makes the upstream calls, which must run once the lock is released.
func (s *Service) apply(ctx context.Context, b *broadcast.Broadcast, events ...Event) (func(context.Context), error) {
	id := b.ID
	st := s.state(id)
	st.Output = b.Output
	var closes []string
	closeIngest := false
	m := Machine{StartTimeout: s.StartTimeout, Grace: s.Grace}
	for len(events) > 0 {
		e := events[0]
		events = events[1:]
		next, cmds := m.Handle(st, e, s.now())
		if next.Output != st.Output {
			if err := s.Broadcasts.SetOutput(ctx, id, next.Output); err != nil {
				return func(context.Context) {}, fmt.Errorf("store the output of broadcast %s: %w", id, err)
			}
		}
		st = next
		for _, c := range cmds {
			switch c.Kind {
			case StartTranscoder:
				if st.Profile == (broadcast.Profile{}) {
					st.Profile = s.quality().Profile(b.Resolution)
				}
				s.setProc(id, "", s.Processes.StartTranscoder(id, st.Profile, s.notifier(id, c, TranscoderFlowing, ProgramLost))) //nolint:contextcheck // process events outlive the request
			case StartRelay:
				t, err := s.Targets.Target(ctx, c.Dest)
				if err != nil {
					events = append(events, Event{Kind: RelayStopped, Run: c.Run, Seq: c.Seq, Dest: c.Dest, Err: s.targetError(id, c.Dest, err)})
					continue
				}
				s.setProc(id, c.Dest, s.Processes.StartRelay(id, t, s.notifier(id, c, RelayFlowing, RelayStopped))) //nolint:contextcheck // as above
			case StopRelay:
				s.stopProcs(id, c.Dest)
			case StopAll:
				s.stopProcs(id, "*")
			case CloseIngest:
				closeIngest = true
				if c.IngestURL != "" {
					closes = append(closes, c.IngestURL)
				}
			case SetOnAir:
				if _, err := s.Stage.SetOnAir(ctx, id, c.On); err != nil {
					s.logger().Warn("set the stage on air", "broadcast", id, "on", c.On, "error", err)
				}
			}
		}
	}
	b.Output = st.Output
	s.mu.Lock()
	s.init()
	if st.Output.State == broadcast.Offline {
		delete(s.states, id)
		s.releaseLocked(id)
	} else {
		s.states[id] = st
	}
	s.mu.Unlock()
	if !closeIngest {
		return func(context.Context) {}, nil
	}
	return func(ctx context.Context) { s.closeIngest(ctx, id, closes) }, nil
}

// notifier turns a process's notifications into events of its run and process number.
func (s *Service) notifier(id string, c Command, flowing, ended EventKind) Notify {
	return func(isFlowing bool, reason string) {
		e := Event{Kind: ended, Run: c.Run, Seq: c.Seq, Dest: c.Dest, Err: reason}
		if isFlowing {
			e.Kind, e.Err = flowing, ""
		} else {
			s.logger().Info("live output process ended", "broadcast", id, "destination", c.Dest, "reason", reason)
		}
		s.dispatch(context.Background(), id, e, true)
	}
}

// targetError is the status text of a destination that cannot be relayed to.
func (s *Service) targetError(id, dest string, err error) string {
	if errors.Is(err, destination.ErrPrivateAddress) {
		return err.Error()
	}
	s.logger().Warn("relay target", "broadcast", id, "destination", dest, "error", err)
	return "The destination cannot be reached."
}

func (s *Service) closeIngest(ctx context.Context, id string, resources []string) {
	ctx, cancel := context.WithTimeout(ctx, UpstreamTimeout)
	defer cancel()
	for _, r := range resources {
		if err := s.Ingest.Close(ctx, r); err != nil {
			s.logger().Warn("close the ingest session", "broadcast", id, "error", err)
		}
	}
	session, err := s.Ingest.Publisher(ctx, id)
	if err == nil && session != "" {
		err = s.kickIfOffline(ctx, id, session)
	}
	if err != nil {
		s.logger().Warn("close the ingest session", "broadcast", id, "error", err)
	}
}

// kickIfOffline closes session when broadcast id is offline or gone; the state is read under
// the lock, the media server is called without it.
func (s *Service) kickIfOffline(ctx context.Context, id, session string) error {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return err
	}
	b, err := s.Broadcasts.Get(ctx, id)
	unlock()
	switch {
	case errors.Is(err, core.ErrNotFound):
	case err != nil:
		return err
	case b.Output.State != broadcast.Offline:
		return nil
	}
	if err := s.Ingest.Kick(ctx, session); err != nil {
		return fmt.Errorf("close the ingest session of broadcast %s: %w", id, err)
	}
	return nil
}

// reserve takes a place under output.max_concurrent and claims the destinations, atomically.
func (s *Service) reserve(id string, dests []destination.Destination) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	if s.closing {
		return conflict("The server is stopping.")
	}
	if len(s.states) >= s.MaxConcurrent {
		return conflict("The server is at its limit of %d live broadcasts.", s.MaxConcurrent)
	}
	for _, d := range dests {
		if other := s.claims[d.ID]; other != "" && other != id {
			return conflict("The destination %q is in use by another broadcast.", d.Name)
		}
	}
	for _, d := range dests {
		s.claims[d.ID] = id
	}
	s.states[id] = State{Output: broadcast.Output{State: broadcast.Starting}}
	return nil
}

func (s *Service) release(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, id)
	s.releaseLocked(id)
}

func (s *Service) releaseLocked(id string) {
	for d, b := range s.claims {
		if b == id {
			delete(s.claims, d)
		}
	}
}

func (s *Service) inUse(ctx context.Context, dest string) error {
	d, err := s.Destinations.Get(ctx, dest)
	if err != nil {
		return conflict("A destination is in use by another broadcast.")
	}
	return conflict("The destination %q is in use by another broadcast.", d.Name)
}

func (s *Service) owned(ctx context.Context, owner, id string) (*broadcast.Broadcast, error) {
	b, err := s.Broadcasts.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.OwnerID != owner {
		return nil, fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	return b, nil
}

func (s *Service) state(id string) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[id]
}

func (s *Service) setProc(id, key string, p Stopper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	if s.procs[id] == nil {
		s.procs[id] = map[string]Stopper{}
	}
	if old := s.procs[id][key]; old != nil {
		old.Stop()
	}
	s.procs[id][key] = p
}

// stopProcs stops broadcast id's process key, or all of them for "*".
func (s *Service) stopProcs(id, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, p := range s.procs[id] {
		if key == "*" || k == key {
			p.Stop()
			delete(s.procs[id], k)
		}
	}
	if len(s.procs[id]) == 0 {
		delete(s.procs, id)
	}
}

func (s *Service) isClosing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing
}

func (s *Service) quality() broadcast.Quality {
	if s.Quality == nil {
		return broadcast.Quality{}
	}
	return s.Quality()
}

func (s *Service) init() {
	if s.states == nil {
		s.states, s.claims, s.procs = map[string]State{}, map[string]string{}, map[string]map[string]Stopper{}
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Logger
}
