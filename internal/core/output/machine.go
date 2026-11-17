// Package output is the live output (design D5): the state machine of a broadcast's output
// with runs, stored deadlines and the relay backoff (Machine, a pure function), and the Service
// that drives it under the broadcast's lock, reserves capacity and destination claims under one
// global lock, proxies the program's WHIP ingest and recovers after a restart. Processes and
// the media server are reached through ports (internal/ffmpeg and internal/mediamtx).
package output

import (
	"maps"
	"slices"
	"time"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
)

// Reasons stored when a broadcast returns to offline on its own.
const (
	ReasonNoProgram   = "no program received"
	ReasonProgramLost = "program lost"
)

// TranscoderRetry is how long after the transcoder ends it is started again while the
// broadcast waits for its program (starting or interrupted).
const TranscoderRetry = time.Second

// relayBackoff is the wait before each retry of a relay that ended or stalled (D5); the last
// value repeats.
var relayBackoff = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second}

// EventKind names an event of the machine.
type EventKind int

// Events (design D5). Every event but GoLive and Recover carries the run it belongs to; process
// events also carry the number of the process they come from.
const (
	GoLive              EventKind = iota + 1 // Dests: the chosen destinations
	TranscoderFlowing                        // Seq
	ProgramLost                              // the transcoder ended or stalled; Seq, Err
	RelayFlowing                             // Seq, Dest
	RelayStopped                             // the relay ended or stalled, or could not start; Seq, Dest, Err
	DestinationsChanged                      // Added, Removed
	End
	Tick
	Recover // the server started with this broadcast not offline; Dests
)

// Event is an input of the machine.
type Event struct {
	Kind           EventKind
	Run            int64
	Seq            int64
	Dest           string
	Err            string // already redacted
	Dests          []string
	Added, Removed []string
}

// CommandKind names a command of the machine.
type CommandKind int

// Commands (design D5).
const (
	StartTranscoder CommandKind = iota + 1 // Run, Seq
	StartRelay                             // Run, Seq, Dest
	StopRelay                              // Dest
	StopAll                                // the transcoder and every relay of the broadcast
	CloseIngest                            // IngestURL: the stored upstream session, maybe ""
	SetOnAir                               // On
)

// Command is an output of the machine for its driver to carry out.
type Command struct {
	Kind      CommandKind
	Run, Seq  int64
	Dest      string
	On        bool
	IngestURL string
}

// Process is the machine's view of one process: the number of the current one and when to
// start the next.
type Process struct {
	Seq     int64
	Running bool
	RetryAt time.Time // zero: no retry pending
}

// Dest is the relay of one chosen destination.
type Dest struct {
	Process
	Status   string // destination.Off, Connecting, Live or Retrying
	Error    string
	Failures int // consecutive, for the backoff
}

// State is a broadcast's output: the stored part (Output) and what lives only in memory.
type State struct {
	Output     broadcast.Output
	StartBy    time.Time // the start timeout, while starting
	Transcoder Process
	Dests      map[string]Dest
	NextSeq    int64 // the last process number given
	// Profile is what the run's transcoder goes out at, fixed by the service at its first start so a retry
	// keeps it when the server's quality changes (add-server-settings, S3); the machine leaves it alone.
	Profile broadcast.Profile
}

// Machine is the output's state machine; Handle is a pure function of its arguments.
type Machine struct {
	StartTimeout time.Duration // output.start_timeout
	Grace        time.Duration // output.reconnect_grace
}

// Handle applies e at now to s and returns the new state and the commands to carry out. s is
// not modified. Events of another run, or of a process that is no longer the current one, are
// ignored, so nothing belonging to an ended run acts afterwards.
func (m Machine) Handle(s State, e Event, now time.Time) (State, []Command) {
	s.Dests = maps.Clone(s.Dests)
	h := handler{m: m, s: s, now: now}
	h.handle(e)
	return h.s, h.cmds
}

type handler struct {
	m    Machine
	s    State
	now  time.Time
	cmds []Command
}

func (h *handler) emit(c Command) { h.cmds = append(h.cmds, c) }

func (h *handler) handle(e Event) {
	o := &h.s.Output
	switch e.Kind {
	case GoLive:
		if o.State != broadcast.Offline {
			return
		}
		o.Run++
		o.State, o.Reason, o.Deadline, o.StartedAt = broadcast.Starting, "", time.Time{}, time.Time{}
		o.IngestID, o.IngestURL = "", ""
		h.s.StartBy = h.now.Add(h.m.StartTimeout)
		h.s.Transcoder, h.s.NextSeq = Process{}, 0
		h.setDests(e.Dests)
		h.startTranscoder()
		return
	case Recover:
		if o.State == broadcast.Offline {
			return
		}
		if o.State != broadcast.Interrupted || o.Deadline.IsZero() {
			o.Deadline = h.now.Add(h.m.Grace)
		}
		o.State = broadcast.Interrupted
		h.s.Transcoder = Process{}
		h.setDests(e.Dests)
		h.emit(Command{Kind: SetOnAir, On: false})
		h.startTranscoder()
		return
	}
	if o.State == broadcast.Offline || (e.Kind != Tick && e.Kind != End && e.Run != o.Run) {
		return
	}
	switch e.Kind {
	case End:
		h.offline("")
	case Tick:
		h.tick()
	case TranscoderFlowing:
		if e.Seq != h.s.Transcoder.Seq || o.State == broadcast.Live {
			return
		}
		o.State, o.Deadline = broadcast.Live, time.Time{}
		if o.StartedAt.IsZero() {
			o.StartedAt = h.now
		}
		h.emit(Command{Kind: SetOnAir, On: true})
		for _, id := range h.ids() {
			h.startRelay(id, destination.Connecting)
		}
	case ProgramLost:
		if e.Seq != h.s.Transcoder.Seq || !h.s.Transcoder.Running {
			return
		}
		h.s.Transcoder = Process{Seq: e.Seq, RetryAt: h.now.Add(TranscoderRetry)}
		if o.State == broadcast.Live {
			o.State, o.Deadline = broadcast.Interrupted, h.now.Add(h.m.Grace)
			h.emit(Command{Kind: SetOnAir, On: false})
			for _, id := range h.ids() {
				d := h.s.Dests[id]
				if d.Running {
					h.emit(Command{Kind: StopRelay, Dest: id})
				}
				h.s.Dests[id] = Dest{Process: Process{Seq: d.Seq}, Status: destination.Off}
			}
		}
	case RelayFlowing:
		d, ok := h.s.Dests[e.Dest]
		if !ok || e.Seq != d.Seq || !d.Running {
			return
		}
		d.Status, d.Error, d.Failures = destination.Live, "", 0
		h.s.Dests[e.Dest] = d
	case RelayStopped:
		d, ok := h.s.Dests[e.Dest]
		if !ok || e.Seq != d.Seq || !d.Running || o.State != broadcast.Live {
			return
		}
		d.Running, d.Status, d.Error = false, destination.Retrying, e.Err
		d.RetryAt = h.now.Add(relayBackoff[min(d.Failures, len(relayBackoff)-1)])
		d.Failures++
		h.s.Dests[e.Dest] = d
	case DestinationsChanged:
		for _, id := range e.Removed {
			if d, ok := h.s.Dests[id]; ok {
				if d.Running {
					h.emit(Command{Kind: StopRelay, Dest: id})
				}
				delete(h.s.Dests, id)
			}
		}
		for _, id := range e.Added {
			if _, ok := h.s.Dests[id]; ok {
				continue
			}
			h.s.Dests[id] = Dest{Status: destination.Off}
			if o.State == broadcast.Live {
				h.startRelay(id, destination.Connecting)
			}
		}
	}
}

func (h *handler) tick() {
	o := &h.s.Output
	switch {
	case o.State == broadcast.Starting && !h.now.Before(h.s.StartBy):
		h.offline(ReasonNoProgram)
		return
	case o.State == broadcast.Interrupted && !h.now.Before(o.Deadline):
		h.offline(ReasonProgramLost)
		return
	}
	if t := h.s.Transcoder; !t.Running && !t.RetryAt.IsZero() && !h.now.Before(t.RetryAt) {
		h.startTranscoder()
	}
	if o.State != broadcast.Live {
		return
	}
	for _, id := range h.ids() {
		if d := h.s.Dests[id]; !d.Running && !d.RetryAt.IsZero() && !h.now.Before(d.RetryAt) {
			h.startRelay(id, d.Status)
		}
	}
}

// offline ends the run: every process stops, the ingest session is closed, the stage is off air.
func (h *handler) offline(reason string) {
	o := &h.s.Output
	url := o.IngestURL
	o.State, o.Reason, o.Deadline, o.IngestID, o.IngestURL = broadcast.Offline, reason, time.Time{}, "", ""
	h.s.StartBy, h.s.Transcoder, h.s.Dests = time.Time{}, Process{}, nil
	h.emit(Command{Kind: StopAll})
	h.emit(Command{Kind: CloseIngest, IngestURL: url})
	h.emit(Command{Kind: SetOnAir, On: false})
}

// ids lists the chosen destinations in order, so the commands come in a stable order.
func (h *handler) ids() []string { return slices.Sorted(maps.Keys(h.s.Dests)) }

func (h *handler) setDests(ids []string) {
	h.s.Dests = make(map[string]Dest, len(ids))
	for _, id := range ids {
		h.s.Dests[id] = Dest{Status: destination.Off}
	}
}

func (h *handler) startTranscoder() {
	h.s.NextSeq++
	h.s.Transcoder = Process{Seq: h.s.NextSeq, Running: true}
	h.emit(Command{Kind: StartTranscoder, Run: h.s.Output.Run, Seq: h.s.NextSeq})
}

// startRelay starts destination id's relay, showing status until it flows.
func (h *handler) startRelay(id, status string) {
	h.s.NextSeq++
	d := h.s.Dests[id]
	d.Process = Process{Seq: h.s.NextSeq, Running: true}
	d.Status = status
	h.s.Dests[id] = d
	h.emit(Command{Kind: StartRelay, Run: h.s.Output.Run, Seq: h.s.NextSeq, Dest: id})
}
