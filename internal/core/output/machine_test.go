package output

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
)

var t0 = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// step is one event at a time after t0, and what must follow: the state, the reason, the
// commands (formatted by cmdString, in order; nil: none) and destination statuses.
type step struct {
	at     time.Duration
	e      Event
	state  broadcast.OutputState
	reason string
	cmds   []string
	dests  map[string]string // destination → status
}

func cmdString(c Command) string {
	switch c.Kind {
	case StartTranscoder:
		return fmt.Sprintf("transcoder r%d s%d", c.Run, c.Seq)
	case StartRelay:
		return fmt.Sprintf("relay %s r%d s%d", c.Dest, c.Run, c.Seq)
	case StopRelay:
		return "stop " + c.Dest
	case StopAll:
		return "stop all"
	case CloseIngest:
		return "close ingest " + c.IngestURL
	case SetOnAir:
		return fmt.Sprintf("on air %v", c.On)
	}
	return "?"
}

// sorted sorts the relay starts and stops, which come from a map in any order.
func sorted(cmds []string) []string {
	out := slices.Clone(cmds)
	slices.SortStableFunc(out, func(a, b string) int {
		ka, kb := strings.HasPrefix(a, "relay ") || strings.HasPrefix(a, "stop d"), strings.HasPrefix(b, "relay ") || strings.HasPrefix(b, "stop d")
		if ka && kb {
			return strings.Compare(a, b)
		}
		return 0
	})
	return out
}

func runSteps(t *testing.T, s State, steps []step) State {
	t.Helper()
	m := Machine{StartTimeout: 30 * time.Second, Grace: 120 * time.Second}
	for i, st := range steps {
		next, cmds := m.Handle(s, st.e, t0.Add(st.at))
		var got []string
		for _, c := range cmds {
			got = append(got, cmdString(c))
		}
		got = sorted(got)
		if !slices.Equal(got, st.cmds) {
			t.Fatalf("step %d (%+v): commands %q, want %q", i, st.e, got, st.cmds)
		}
		if next.Output.State != st.state || next.Output.Reason != st.reason {
			t.Fatalf("step %d: state %s %q, want %s %q", i, next.Output.State, next.Output.Reason, st.state, st.reason)
		}
		for d, want := range st.dests {
			if got := next.Dests[d].Status; got != want {
				t.Fatalf("step %d: destination %s is %q, want %q", i, d, got, want)
			}
		}
		s = next
	}
	return s
}

var (
	offline = State{Output: broadcast.Output{State: broadcast.Offline, Run: 1}}
	dAB     = []string{"dA", "dB"}
	off     = destination.Off
	conn    = destination.Connecting
	live    = destination.Live
	retry   = destination.Retrying
)

// goLive is the common start: run 2, transcoder s1 flowing at 3 s, relays s2 (dA) and s3 (dB).
var goLive = []step{
	{0, Event{Kind: GoLive, Dests: dAB}, broadcast.Starting, "", []string{"transcoder r2 s1"}, map[string]string{"dA": off}},
	{3 * time.Second, Event{Kind: TranscoderFlowing, Run: 2, Seq: 1}, broadcast.Live, "",
		[]string{"on air true", "relay dA r2 s2", "relay dB r2 s3"}, map[string]string{"dA": conn, "dB": conn}},
	{4 * time.Second, Event{Kind: RelayFlowing, Run: 2, Seq: 2, Dest: "dA"}, broadcast.Live, "", nil, map[string]string{"dA": live, "dB": conn}},
	{4 * time.Second, Event{Kind: RelayFlowing, Run: 2, Seq: 3, Dest: "dB"}, broadcast.Live, "", nil, map[string]string{"dA": live, "dB": live}},
}

func TestMachine(t *testing.T) {
	t.Parallel()
	S, L, I, O := broadcast.Starting, broadcast.Live, broadcast.Interrupted, broadcast.Offline
	tests := []struct {
		name  string
		steps []step
	}{
		{"go live to two destinations", goLive},
		{"process alone is not live; no program", []step{
			{0, Event{Kind: GoLive, Dests: dAB}, S, "", []string{"transcoder r2 s1"}, nil},
			{10 * time.Second, Event{Kind: Tick}, S, "", nil, nil},
			{29 * time.Second, Event{Kind: Tick}, S, "", nil, nil},
			{30 * time.Second, Event{Kind: Tick}, O, ReasonNoProgram, []string{"stop all", "close ingest ", "on air false"}, nil},
		}},
		{"the transcoder is retried every second while starting", []step{
			{0, Event{Kind: GoLive, Dests: dAB}, S, "", []string{"transcoder r2 s1"}, nil},
			{100 * time.Millisecond, Event{Kind: ProgramLost, Run: 2, Seq: 1, Err: "404"}, S, "", nil, nil},
			{900 * time.Millisecond, Event{Kind: Tick}, S, "", nil, nil},
			{1100 * time.Millisecond, Event{Kind: Tick}, S, "", []string{"transcoder r2 s2"}, nil},
			{1200 * time.Millisecond, Event{Kind: ProgramLost, Run: 2, Seq: 1}, S, "", nil, nil}, // the old process: ignored
			{2 * time.Second, Event{Kind: TranscoderFlowing, Run: 2, Seq: 2}, L, "", []string{"on air true", "relay dA r2 s3", "relay dB r2 s4"}, nil},
		}},
		{"one destination fails with the backoff, the other is untouched", append(slices.Clone(goLive),
			step{5 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 3, Dest: "dB", Err: "refused"}, L, "", nil, map[string]string{"dA": live, "dB": retry}},
			step{5900 * time.Millisecond, Event{Kind: Tick}, L, "", nil, nil},
			step{6 * time.Second, Event{Kind: Tick}, L, "", []string{"relay dB r2 s4"}, map[string]string{"dB": retry}},
			step{6 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 4, Dest: "dB", Err: "refused"}, L, "", nil, nil},
			step{7900 * time.Millisecond, Event{Kind: Tick}, L, "", nil, nil},
			step{8 * time.Second, Event{Kind: Tick}, L, "", []string{"relay dB r2 s5"}, nil},
			step{8 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 5, Dest: "dB"}, L, "", nil, nil},
			step{13 * time.Second, Event{Kind: Tick}, L, "", []string{"relay dB r2 s6"}, nil},
			step{13 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 6, Dest: "dB"}, L, "", nil, nil},
			step{22 * time.Second, Event{Kind: Tick}, L, "", nil, nil},
			step{23 * time.Second, Event{Kind: Tick}, L, "", []string{"relay dB r2 s7"}, nil},
			step{23 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 7, Dest: "dB"}, L, "", nil, nil},
			step{33 * time.Second, Event{Kind: Tick}, L, "", []string{"relay dB r2 s8"}, nil},
			// it recovers at the next retry
			step{34 * time.Second, Event{Kind: RelayFlowing, Run: 2, Seq: 8, Dest: "dB"}, L, "", nil, map[string]string{"dA": live, "dB": live}},
		)},
		{"program lost and back", append(slices.Clone(goLive),
			step{10 * time.Second, Event{Kind: ProgramLost, Run: 2, Seq: 1}, I, "",
				[]string{"on air false", "stop dA", "stop dB"}, map[string]string{"dA": off, "dB": off}},
			step{10 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 2, Dest: "dA"}, I, "", nil, map[string]string{"dA": off}},
			step{11 * time.Second, Event{Kind: Tick}, I, "", []string{"transcoder r2 s4"}, nil},
			step{20 * time.Second, Event{Kind: TranscoderFlowing, Run: 2, Seq: 4}, L, "",
				[]string{"on air true", "relay dA r2 s5", "relay dB r2 s6"}, map[string]string{"dA": conn, "dB": conn}},
		)},
		{"owner never returns", append(slices.Clone(goLive),
			step{10 * time.Second, Event{Kind: ProgramLost, Run: 2, Seq: 1}, I, "", []string{"on air false", "stop dA", "stop dB"}, nil},
			step{129 * time.Second, Event{Kind: Tick}, I, "", []string{"transcoder r2 s4"}, nil},
			step{130 * time.Second, Event{Kind: Tick}, O, ReasonProgramLost, []string{"stop all", "close ingest ", "on air false"}, nil},
		)},
		{"end while live, no retry after end", append(slices.Clone(goLive),
			step{5 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 3, Dest: "dB"}, L, "", nil, nil},
			step{5 * time.Second, Event{Kind: End}, O, "", []string{"stop all", "close ingest ", "on air false"}, nil},
			step{20 * time.Second, Event{Kind: Tick}, O, "", nil, nil},
			step{20 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 2, Dest: "dA"}, O, "", nil, nil},
		)},
		{"end while starting", []step{
			{0, Event{Kind: GoLive, Dests: dAB}, S, "", []string{"transcoder r2 s1"}, nil},
			{time.Second, Event{Kind: End}, O, "", []string{"stop all", "close ingest ", "on air false"}, nil},
		}},
		{"end then go live again: late events of the first run change nothing", append(slices.Clone(goLive),
			step{5 * time.Second, Event{Kind: End}, O, "", []string{"stop all", "close ingest ", "on air false"}, nil},
			step{5 * time.Second, Event{Kind: GoLive, Dests: dAB}, S, "", []string{"transcoder r3 s1"}, nil},
			step{6 * time.Second, Event{Kind: TranscoderFlowing, Run: 2, Seq: 1}, S, "", nil, nil},
			step{6 * time.Second, Event{Kind: ProgramLost, Run: 2, Seq: 1}, S, "", nil, nil},
			step{6 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 2, Dest: "dA"}, S, "", nil, nil},
			step{7 * time.Second, Event{Kind: Tick}, S, "", nil, nil},
		)},
		{"add and remove while live", append(slices.Clone(goLive),
			step{5 * time.Second, Event{Kind: DestinationsChanged, Run: 2, Added: []string{"dC"}}, L, "", []string{"relay dC r2 s4"},
				map[string]string{"dA": live, "dB": live, "dC": conn}},
			step{6 * time.Second, Event{Kind: DestinationsChanged, Run: 2, Removed: []string{"dA"}}, L, "", []string{"stop dA"},
				map[string]string{"dA": "", "dB": live}},
			step{6 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 2, Dest: "dA"}, L, "", nil, map[string]string{"dA": ""}},
		)},
		{"destinations change while starting: relays wait for the program", []step{
			{0, Event{Kind: GoLive, Dests: dAB}, S, "", []string{"transcoder r2 s1"}, nil},
			{time.Second, Event{Kind: DestinationsChanged, Run: 2, Added: []string{"dC"}, Removed: []string{"dA"}}, S, "", nil,
				map[string]string{"dA": "", "dC": off}},
			{2 * time.Second, Event{Kind: TranscoderFlowing, Run: 2, Seq: 1}, L, "", []string{"on air true", "relay dB r2 s2", "relay dC r2 s3"}, nil},
		}},
		{"stale events are ignored", append(slices.Clone(goLive),
			step{5 * time.Second, Event{Kind: RelayStopped, Run: 1, Seq: 3, Dest: "dB"}, L, "", nil, map[string]string{"dB": live}},
			step{5 * time.Second, Event{Kind: RelayStopped, Run: 2, Seq: 9, Dest: "dB"}, L, "", nil, map[string]string{"dB": live}},
			step{5 * time.Second, Event{Kind: ProgramLost, Run: 2, Seq: 9}, L, "", nil, nil},
			step{5 * time.Second, Event{Kind: GoLive, Dests: dAB}, L, "", nil, nil},
		)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runSteps(t, offline, tt.steps)
		})
	}
}

func TestMachineRecover(t *testing.T) {
	t.Parallel()
	I, L, O := broadcast.Interrupted, broadcast.Live, broadcast.Offline
	stored := func(st broadcast.OutputState, deadline time.Time) State {
		return State{Output: broadcast.Output{State: st, Run: 4, Deadline: deadline, IngestURL: "http://m/r1", StartedAt: t0.Add(-time.Hour)}}
	}
	t.Run("live or starting: interrupted with a new deadline, then live again", func(t *testing.T) {
		t.Parallel()
		for _, st := range []broadcast.OutputState{broadcast.Live, broadcast.Starting} {
			s := runSteps(t, stored(st, time.Time{}), []step{
				{0, Event{Kind: Recover, Dests: dAB}, I, "", []string{"on air false", "transcoder r4 s1"}, map[string]string{"dA": off}},
			})
			if !s.Output.Deadline.Equal(t0.Add(120 * time.Second)) {
				t.Fatalf("deadline %v", s.Output.Deadline)
			}
			s = runSteps(t, s, []step{
				{2 * time.Second, Event{Kind: TranscoderFlowing, Run: 4, Seq: 1}, L, "", []string{"on air true", "relay dA r4 s2", "relay dB r4 s3"}, nil},
			})
			if s.Output.IngestURL != "http://m/r1" || !s.Output.StartedAt.Equal(t0.Add(-time.Hour)) {
				t.Fatalf("the ingest session or the start time was lost: %+v", s.Output)
			}
		}
	})
	t.Run("restarts do not extend an outage", func(t *testing.T) {
		t.Parallel()
		deadline := t0.Add(30 * time.Second)
		s := runSteps(t, stored(broadcast.Interrupted, deadline), []step{
			{0, Event{Kind: Recover, Dests: dAB}, I, "", []string{"on air false", "transcoder r4 s1"}, nil},
			{0, Event{Kind: ProgramLost, Run: 4, Seq: 1}, I, "", nil, nil},
			{29 * time.Second, Event{Kind: Tick}, I, "", []string{"transcoder r4 s2"}, nil},
		})
		if !s.Output.Deadline.Equal(deadline) {
			t.Fatalf("deadline %v, want %v", s.Output.Deadline, deadline)
		}
		// a second restart keeps it too
		s = runSteps(t, s, []step{
			{29 * time.Second, Event{Kind: Recover, Dests: dAB}, I, "", []string{"on air false", "transcoder r4 s3"}, nil},
			{30 * time.Second, Event{Kind: Tick}, O, ReasonProgramLost, []string{"stop all", "close ingest http://m/r1", "on air false"}, nil},
		})
	})
	t.Run("offline stays offline", func(t *testing.T) {
		t.Parallel()
		runSteps(t, offline, []step{{0, Event{Kind: Recover, Dests: dAB}, O, "", nil, nil}})
	})
}

func TestHandleDoesNotModifyItsInput(t *testing.T) {
	t.Parallel()
	m := Machine{StartTimeout: time.Second, Grace: time.Second}
	s, _ := m.Handle(offline, Event{Kind: GoLive, Dests: dAB}, t0)
	before := s.Dests["dA"]
	_, _ = m.Handle(s, Event{Kind: DestinationsChanged, Run: s.Output.Run, Removed: []string{"dA"}}, t0)
	if s.Dests["dA"] != before {
		t.Fatal("Handle changed the state it was given")
	}
}
