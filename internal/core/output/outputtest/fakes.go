// Package outputtest holds in-memory fakes of the live output's ports and an environment that
// assembles an output.Service on them with a controlled clock, for the core's and the
// handlers' tests. The real adapters are internal/store, internal/ffmpeg and internal/mediamtx.
package outputtest

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/core/output"
	"github.com/jniltinho/streamgo/internal/core/stage"
)

// Broadcasts is an in-memory broadcast.Repository.
type Broadcasts struct {
	broadcast.Repository
	mu   sync.Mutex
	rows map[string]broadcast.Broadcast
}

// Put stores b as it is.
func (f *Broadcasts) Put(b broadcast.Broadcast) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows = map[string]broadcast.Broadcast{}
	}
	f.rows[b.ID] = b
}

// Get returns a copy of the broadcast.
func (f *Broadcasts) Get(_ context.Context, id string) (*broadcast.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	return &b, nil
}

// ListByOutputState lists the broadcasts in any of the states, by id.
func (f *Broadcasts) ListByOutputState(_ context.Context, states ...broadcast.OutputState) ([]broadcast.Broadcast, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []broadcast.Broadcast
	for _, b := range f.rows {
		if slices.Contains(states, b.Output.State) {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b broadcast.Broadcast) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// SetOutput stores the output.
func (f *Broadcasts) SetOutput(_ context.Context, id string, o broadcast.Output) error {
	return f.update(id, func(b *broadcast.Broadcast) { b.Output = o })
}

// SetStage stores the stage state.
func (f *Broadcasts) SetStage(_ context.Context, id, s string, v int64) error {
	return f.update(id, func(b *broadcast.Broadcast) { b.Stage, b.StageVersion = s, v })
}

func (f *Broadcasts) update(id string, fn func(*broadcast.Broadcast)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.rows[id]
	if !ok {
		return fmt.Errorf("broadcast %s: %w", id, core.ErrNotFound)
	}
	fn(&b)
	f.rows[id] = b
	return nil
}

// Destinations is an in-memory destination.Repository with the selections; InUse reads the
// broadcasts.
type Destinations struct {
	destination.Repository
	Broadcasts *Broadcasts
	mu         sync.Mutex
	rows       map[string]destination.Destination
	selected   map[string][]string
}

// Create stores d with the id it carries.
func (f *Destinations) Create(_ context.Context, d *destination.Destination) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rows == nil {
		f.rows, f.selected = map[string]destination.Destination{}, map[string][]string{}
	}
	f.rows[d.ID] = *d
	return nil
}

// Get returns the destination.
func (f *Destinations) Get(_ context.Context, id string) (*destination.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.rows[id]
	if !ok {
		return nil, fmt.Errorf("destination %s: %w", id, core.ErrNotFound)
	}
	return &d, nil
}

// InUse reports whether a broadcast that is not offline chose the destination.
func (f *Destinations) InUse(ctx context.Context, id string) (bool, error) {
	f.mu.Lock()
	var owners []string
	for b, ids := range f.selected {
		if slices.Contains(ids, id) {
			owners = append(owners, b)
		}
	}
	f.mu.Unlock()
	for _, b := range owners {
		if row, err := f.Broadcasts.Get(ctx, b); err == nil && row.Output.State != broadcast.Offline {
			return true, nil
		}
	}
	return false, nil
}

// ListByBroadcast lists the broadcast's chosen destinations by id.
func (f *Destinations) ListByBroadcast(_ context.Context, broadcastID string) ([]destination.Destination, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []destination.Destination
	for _, id := range f.selected[broadcastID] {
		out = append(out, f.rows[id])
	}
	return out, nil
}

// SetSelection replaces the broadcast's chosen destinations.
func (f *Destinations) SetSelection(_ context.Context, broadcastID string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.selected[broadcastID] = slices.Sorted(slices.Values(ids))
	return nil
}

// Proc is a started fake process.
type Proc struct {
	Broadcast string
	Dest      string // "" for the transcoder
	Target    string // the relay's URL
	Notify    output.Notify
	mu        sync.Mutex
	stopped   bool
}

// Stop records the stop.
func (p *Proc) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
}

// Stopped reports whether Stop was called.
func (p *Proc) Stopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// Flow reports the process flowing.
func (p *Proc) Flow() { p.Notify(true, "") }

// Exit reports the process ended.
func (p *Proc) Exit(reason string) { p.Notify(false, reason) }

// Processes records the processes it is asked to start.
type Processes struct {
	mu       sync.Mutex
	started  []*Proc
	stopAlls int
}

// StartTranscoder records a transcoder.
func (f *Processes) StartTranscoder(id string, notify output.Notify) output.Stopper {
	return f.add(&Proc{Broadcast: id, Notify: notify})
}

// StartRelay records a relay.
func (f *Processes) StartRelay(id string, t destination.Target, notify output.Notify) output.Stopper {
	dest := t.URL[strings.LastIndex(t.URL, "/")+1:]
	return f.add(&Proc{Broadcast: id, Dest: dest, Target: t.URL, Notify: notify})
}

// StopAll stops every process.
func (f *Processes) StopAll() {
	f.mu.Lock()
	f.stopAlls++
	list := slices.Clone(f.started)
	f.mu.Unlock()
	for _, p := range list {
		p.Stop()
	}
}

// StopAlls is how many times StopAll ran.
func (f *Processes) StopAlls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopAlls
}

func (f *Processes) add(p *Proc) *Proc {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, p)
	return p
}

// Started lists the processes of broadcast id for dest ("" for transcoders), oldest first.
func (f *Processes) Started(id, dest string) []*Proc {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*Proc
	for _, p := range f.started {
		if p.Broadcast == id && p.Dest == dest {
			out = append(out, p)
		}
	}
	return out
}

// Last is the newest process of broadcast id for dest; it fails the test when there is none.
func (f *Processes) Last(t testing.TB, id, dest string) *Proc {
	t.Helper()
	list := f.Started(id, dest)
	if len(list) == 0 {
		t.Fatalf("no process started for broadcast %s destination %q", id, dest)
	}
	return list[len(list)-1]
}

// Ingest is an in-memory media server: one publisher per broadcast.
type Ingest struct {
	mu         sync.Mutex
	n          int
	publishers map[string]string // broadcast id → session
	resources  map[string]string // resource → broadcast id, while open
	Closed     []string          // resources closed
	Kicked     []string          // sessions kicked
	Calls      int               // calls of any method
	// Hook, when set, runs inside Publish before it answers; an error makes Publish fail.
	Hook func(broadcastID string) error
}

// Publish opens a session that replaces the broadcast's previous publisher.
func (f *Ingest) Publish(_ context.Context, id string, offer []byte) (output.Answer, error) {
	f.mu.Lock()
	f.Calls++
	hook := f.Hook
	f.mu.Unlock()
	if hook != nil {
		if err := hook(id); err != nil {
			return output.Answer{}, fmt.Errorf("%w: %w", output.ErrUpstream, err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishers == nil {
		f.publishers, f.resources = map[string]string{}, map[string]string{}
	}
	f.n++
	res := fmt.Sprintf("http://mediamtx.test/program/%s/whip/r%d", id, f.n)
	f.resources[res] = id
	f.publishers[id] = fmt.Sprintf("s%d", f.n)
	return output.Answer{SDP: append([]byte("answer to "), offer...), Resource: res, ETag: "*", Links: []string{"<stun:x>; rel=\"ice-server\""}}, nil
}

// Close closes a resource; the broadcast's publisher goes with it when it is that resource's.
func (f *Ingest) Close(_ context.Context, res string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	f.Closed = append(f.Closed, res)
	if id, ok := f.resources[res]; ok {
		delete(f.resources, res)
		if strings.HasSuffix(res, "/r"+strings.TrimPrefix(f.publishers[id], "s")) {
			delete(f.publishers, id)
		}
	}
	return nil
}

// Publisher is the broadcast's publishing session.
func (f *Ingest) Publisher(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	return f.publishers[id], nil
}

// Publishing maps each broadcast to its publishing session.
func (f *Ingest) Publishing(context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	out := map[string]string{}
	for id, s := range f.publishers {
		out[id] = s
	}
	return out, nil
}

// Kick closes a session.
func (f *Ingest) Kick(_ context.Context, session string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	f.Kicked = append(f.Kicked, session)
	for id, s := range f.publishers {
		if s == session {
			delete(f.publishers, id)
		}
	}
	return nil
}

// SetPublisher makes session the broadcast's publisher, as if it published directly.
func (f *Ingest) SetPublisher(id, session string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.publishers == nil {
		f.publishers, f.resources = map[string]string{}, map[string]string{}
	}
	f.publishers[id] = session
}

// PublisherOf is the broadcast's publishing session, "" when none.
func (f *Ingest) PublisherOf(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.publishers[id]
}

// Snapshot returns copies of Closed, Kicked and Calls.
func (f *Ingest) Snapshot() (closed, kicked []string, calls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Closed), slices.Clone(f.Kicked), f.Calls
}

// Clock is a controlled clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// Now returns the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type pusher struct{}

func (pusher) UpdateMetadata(context.Context, string, string) error { return nil }

type sealer struct{}

func (sealer) Seal(_ string, plain []byte) ([]byte, error)  { return plain, nil }
func (sealer) Open(_ string, sealed []byte) ([]byte, error) { return sealed, nil }

type lookup struct{ e *Env }

func (l lookup) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	l.e.mu.Lock()
	defer l.e.mu.Unlock()
	a, ok := l.e.resolverHost[host]
	if !ok {
		return nil, fmt.Errorf("lookup %s: no such host", host)
	}
	return []netip.Addr{netip.MustParseAddr(a)}, nil
}

// Env is an output.Service on fakes, with the real destination service and stage writer.
type Env struct {
	Service      *output.Service
	Broadcasts   *Broadcasts
	Dests        *Destinations
	DestService  *destination.Service
	Processes    *Processes
	Ingest       *Ingest
	Clock        *Clock
	Locks        *broadcast.Locks
	mu           sync.Mutex
	resolverHost map[string]string
}

// Durations of the Env's service.
const (
	StartTimeout = 30 * time.Second
	Grace        = 120 * time.Second
)

// New returns an Env with MaxConcurrent 2.
func New(t testing.TB) *Env {
	t.Helper()
	e := &Env{
		Broadcasts: &Broadcasts{}, Processes: &Processes{}, Ingest: &Ingest{}, Locks: &broadcast.Locks{},
		Clock: &Clock{now: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}, resolverHost: map[string]string{},
	}
	e.Dests = &Destinations{Broadcasts: e.Broadcasts}
	_ = e.Dests.Create(context.Background(), &destination.Destination{ID: "none"})
	e.DestService = &destination.Service{
		Repo: e.Dests, Broadcasts: e.Broadcasts, Locks: e.Locks, Sealer: sealer{}, MaxPerBroadcast: 5,
		Resolver: lookup{e},
	}
	writer := &stage.Writer{Broadcasts: e.Broadcasts, Locks: e.Locks, Rooms: pusher{}, MaxOnStage: 6}
	e.Service = &output.Service{
		Broadcasts: e.Broadcasts, Locks: e.Locks, Destinations: e.Dests, Targets: e.DestService,
		Stage: writer, Ingest: e.Ingest, Processes: e.Processes, MaxConcurrent: 2,
		StartTimeout: StartTimeout, Grace: Grace, Now: e.Clock.Now,
	}
	e.DestService.Statuses, e.DestService.Notifier = e.Service, e.Service
	return e
}

// AddBroadcast stores an offline broadcast of owner choosing the destinations.
func (e *Env) AddBroadcast(t testing.TB, id, owner string, dests ...string) {
	t.Helper()
	e.Broadcasts.Put(broadcast.Broadcast{ID: id, OwnerID: owner, Title: id, Stage: broadcast.DefaultStage,
		Output: broadcast.Output{State: broadcast.Offline}})
	if err := e.Dests.SetSelection(context.Background(), id, dests); err != nil {
		t.Fatal(err)
	}
}

// AddDestination stores a destination of owner at rtmp://<host>/<id>, or at the public
// dest.test when host is "". Its relay's fake process carries the destination id.
func (e *Env) AddDestination(t testing.TB, id, owner, name, host string) {
	t.Helper()
	if host == "" {
		host = "dest.test"
	}
	e.Resolve(host, "203.0.113.7")
	addr := "rtmp://" + host
	_ = e.Dests.Create(context.Background(), &destination.Destination{ID: id, OwnerID: owner, Name: name,
		Scheme: "rtmp", Host: host, Sealed: []byte(addr + "\n" + id)})
}

// Resolve makes host resolve to addr from now on.
func (e *Env) Resolve(host, addr string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resolverHost[host] = addr
}

// Output is the stored output of broadcast id.
func (e *Env) Output(t testing.TB, id string) broadcast.Output {
	t.Helper()
	b, err := e.Broadcasts.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return b.Output
}

// OnAir is the stored stage's on_air of broadcast id.
func (e *Env) OnAir(t testing.TB, id string) bool {
	t.Helper()
	b, err := e.Broadcasts.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s, err := stage.Parse(b.Stage)
	if err != nil {
		t.Fatal(err)
	}
	return s.OnAir
}

// Status is destination dest's status in broadcast id.
func (e *Env) Status(id, dest string) destination.Status {
	st, ok := e.Service.StatusOf(id)[dest]
	if !ok {
		return destination.Status{State: destination.Off}
	}
	return st
}

// Tick advances the clock by d and lets the service check its timers.
func (e *Env) Tick(d time.Duration) {
	e.Clock.Advance(d)
	e.Service.Tick(context.Background())
}
