package output_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/core/output"
	"github.com/jniltinho/streamgo/internal/core/output/outputtest"
)

var ctx = context.Background()

// eventually waits for cond, which background work makes true.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 200 {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func conflictMessage(t *testing.T, err error) string {
	t.Helper()
	var c *output.Conflict
	if !errors.As(err, &c) || !errors.Is(err, core.ErrConflict) {
		t.Fatalf("error %v, want a conflict", err)
	}
	return c.Message
}

// liveEnv is broadcast b1 of u1 live on destinations dA and dB, both flowing.
func liveEnv(t *testing.T) *outputtest.Env {
	t.Helper()
	e := outputtest.New(t)
	e.AddDestination(t, "dA", "u1", "YouTube", "")
	e.AddDestination(t, "dB", "u1", "Twitch", "")
	e.AddBroadcast(t, "b1", "u1", "dA", "dB")
	o, err := e.Service.GoLive(ctx, "u1", "b1")
	if err != nil || o.State != broadcast.Starting {
		t.Fatalf("go live: %+v %v", o, err)
	}
	e.Processes.Last(t, "b1", "").Flow()
	e.Processes.Last(t, "b1", "dA").Flow()
	e.Processes.Last(t, "b1", "dB").Flow()
	return e
}

func TestGoLive(t *testing.T) {
	t.Parallel()
	e := outputtest.New(t)
	e.AddDestination(t, "dA", "u1", "YouTube", "")
	e.AddDestination(t, "dB", "u1", "Twitch", "")
	e.AddBroadcast(t, "b1", "u1", "dA", "dB")
	if _, err := e.Service.GoLive(ctx, "u2", "b1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another user: %v", err)
	}
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	// process alone is not live
	if e.Output(t, "b1").State != broadcast.Starting || len(e.Processes.Started("b1", "dA")) != 0 {
		t.Fatal("a running transcoder made the broadcast live")
	}
	e.Tick(10 * time.Second)
	if e.Output(t, "b1").State != broadcast.Starting {
		t.Fatal("not starting")
	}
	e.Processes.Last(t, "b1", "").Flow()
	if o := e.Output(t, "b1"); o.State != broadcast.Live || o.StartedAt.IsZero() || !e.OnAir(t, "b1") {
		t.Fatalf("after flowing: %+v on air %v", o, e.OnAir(t, "b1"))
	}
	if st := e.Status("b1", "dA"); st.State != destination.Connecting {
		t.Fatalf("dA %+v", st)
	}
	e.Processes.Last(t, "b1", "dA").Flow()
	e.Processes.Last(t, "b1", "dB").Flow()
	for _, d := range []string{"dA", "dB"} {
		if st := e.Status("b1", d); st.State != destination.Live {
			t.Fatalf("%s %+v", d, st)
		}
	}
	if !strings.Contains(e.Processes.Last(t, "b1", "dA").Target, "rtmp://dest.test/dA") {
		t.Fatal("the relay does not publish to the destination")
	}
	// statuses are private: the stage that every participant receives names no destination
	b, _ := e.Broadcasts.Get(ctx, "b1")
	for _, secret := range []string{"YouTube", "Twitch", "dest.test", "dA", "rtmp"} {
		if strings.Contains(b.Stage, secret) {
			t.Fatalf("the stage holds %q: %s", secret, b.Stage)
		}
	}
	// double go live
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err == nil {
		t.Fatal("a second go live passed")
	}
}

func TestGoLiveRefused(t *testing.T) {
	t.Parallel()
	t.Run("no destination", func(t *testing.T) {
		t.Parallel()
		e := outputtest.New(t)
		e.AddBroadcast(t, "b1", "u1")
		_, err := e.Service.GoLive(ctx, "u1", "b1")
		if msg := conflictMessage(t, err); !strings.Contains(msg, "destination") {
			t.Fatal(msg)
		}
		if e.Output(t, "b1").State != broadcast.Offline || len(e.Processes.Started("b1", "")) != 0 {
			t.Fatal("something started")
		}
	})
	t.Run("destination already in use", func(t *testing.T) {
		t.Parallel()
		e := liveEnv(t)
		e.AddBroadcast(t, "b2", "u1", "dB")
		_, err := e.Service.GoLive(ctx, "u1", "b2")
		if msg := conflictMessage(t, err); !strings.Contains(msg, `"Twitch"`) {
			t.Fatalf("the message does not name the destination: %s", msg)
		}
	})
}

// TestConcurrency: two owners at once with max_concurrent 1, and two go lives of one broadcast.
func TestConcurrency(t *testing.T) {
	t.Parallel()
	for range 20 {
		e := outputtest.New(t)
		e.Service.MaxConcurrent = 1
		e.AddDestination(t, "dA", "u1", "A", "")
		e.AddDestination(t, "dB", "u2", "B", "")
		e.AddBroadcast(t, "b1", "u1", "dA")
		e.AddBroadcast(t, "b2", "u2", "dB")
		errs := parallel(func() error { _, err := e.Service.GoLive(ctx, "u1", "b1"); return err },
			func() error { _, err := e.Service.GoLive(ctx, "u2", "b2"); return err })
		ok, msgs := tally(t, errs)
		if ok != 1 || !strings.Contains(msgs, "at its limit") {
			t.Fatalf("%d succeeded: %s", ok, msgs)
		}

		e = outputtest.New(t)
		e.AddDestination(t, "dA", "u1", "A", "")
		e.AddBroadcast(t, "b1", "u1", "dA")
		errs = parallel(func() error { _, err := e.Service.GoLive(ctx, "u1", "b1"); return err },
			func() error { _, err := e.Service.GoLive(ctx, "u1", "b1"); return err })
		if ok, msgs := tally(t, errs); ok != 1 || len(e.Processes.Started("b1", "")) != 1 {
			t.Fatalf("%d succeeded, %d transcoders: %s", ok, len(e.Processes.Started("b1", "")), msgs)
		}
	}
}

// TestAddDestinationInUseAtTheSameInstant: A adds the destination B goes live with, at once.
func TestAddDestinationInUseAtTheSameInstant(t *testing.T) {
	t.Parallel()
	for range 20 {
		e := outputtest.New(t)
		e.AddDestination(t, "dA", "u1", "A", "")
		e.AddDestination(t, "dX", "u1", "Shared", "")
		e.AddBroadcast(t, "a", "u1", "dA")
		e.AddBroadcast(t, "b", "u1", "dX")
		if _, err := e.Service.GoLive(ctx, "u1", "a"); err != nil {
			t.Fatal(err)
		}
		errs := parallel(func() error { _, err := e.DestService.Select(ctx, "u1", "a", []string{"dA", "dX"}); return err },
			func() error { _, err := e.Service.GoLive(ctx, "u1", "b"); return err })
		if ok, msgs := tally(t, errs); ok != 1 || !strings.Contains(msgs, "in use") {
			t.Fatalf("%d succeeded: %s", ok, msgs)
		}
	}
}

func TestChangeDestinationsWhileLive(t *testing.T) {
	t.Parallel()
	e := liveEnv(t)
	e.AddDestination(t, "dC", "u1", "Facebook", "")
	relayA := e.Processes.Last(t, "b1", "dA")
	if _, err := e.DestService.Select(ctx, "u1", "b1", []string{"dA", "dB", "dC"}); err != nil {
		t.Fatal(err)
	}
	e.Processes.Last(t, "b1", "dC").Flow()
	if e.Status("b1", "dC").State != destination.Live || e.Processes.Last(t, "b1", "dA") != relayA || relayA.Stopped() {
		t.Fatal("adding touched another relay, or the new one is not live")
	}
	if _, err := e.DestService.Select(ctx, "u1", "b1", []string{"dB", "dC"}); err != nil {
		t.Fatal(err)
	}
	if !relayA.Stopped() || e.Processes.Last(t, "b1", "dB").Stopped() || e.Status("b1", "dB").State != destination.Live {
		t.Fatal("removing dA did not stop it alone")
	}
	if e.Status("b1", "dA").State != destination.Off {
		t.Fatal("a removed destination keeps a status")
	}
	_, err := e.DestService.Select(ctx, "u1", "b1", nil)
	conflictMessage(t, err)
	// the removed destination is free for another broadcast, the others are not
	e.AddBroadcast(t, "b2", "u1", "dA")
	if _, err := e.Service.GoLive(ctx, "u1", "b2"); err != nil {
		t.Fatalf("the released destination: %v", err)
	}
}

func TestRelayFailures(t *testing.T) {
	t.Parallel()
	e := liveEnv(t)
	relayA := e.Processes.Last(t, "b1", "dA")
	e.Processes.Last(t, "b1", "dB").Exit("rtmp://dest.test/***: Connection refused")
	if st := e.Status("b1", "dB"); st.State != destination.Retrying || !strings.Contains(st.Error, "refused") {
		t.Fatalf("dB %+v", st)
	}
	if relayA.Stopped() || e.Status("b1", "dA").State != destination.Live {
		t.Fatal("the working destination was interrupted")
	}
	e.Tick(time.Second)
	if n := len(e.Processes.Started("b1", "dB")); n != 2 {
		t.Fatalf("%d relays for dB, want a retry after 1 s", n)
	}
	e.Processes.Last(t, "b1", "dB").Flow()
	if e.Status("b1", "dB").State != destination.Live {
		t.Fatal("the destination did not recover")
	}

	// no retry after end
	e.Processes.Last(t, "b1", "dB").Exit("refused")
	if _, err := e.Service.End(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	e.Tick(time.Minute)
	if n := len(e.Processes.Started("b1", "dB")); n != 2 {
		t.Fatalf("a relay started after end (%d)", n)
	}
}

// TestPrivateAddressAtRelayStart: a saved destination whose host now resolves to 10.0.0.5.
func TestPrivateAddressAtRelayStart(t *testing.T) {
	t.Parallel()
	e := outputtest.New(t)
	e.AddDestination(t, "dA", "u1", "A", "")
	e.AddDestination(t, "dP", "u1", "Private", "later.example.com")
	e.AddBroadcast(t, "b1", "u1", "dA", "dP")
	e.Resolve("later.example.com", "10.0.0.5")
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	e.Processes.Last(t, "b1", "").Flow()
	if n := len(e.Processes.Started("b1", "dP")); n != 0 {
		t.Fatal("a relay was started to a private address")
	}
	st := e.Status("b1", "dP")
	if st.State != destination.Retrying || !strings.Contains(st.Error, "private address") {
		t.Fatalf("dP %+v", st)
	}
	e.Tick(10 * time.Second)
	if n := len(e.Processes.Started("b1", "dP")); n != 0 || e.Status("b1", "dA").State != destination.Connecting {
		t.Fatal("retried into a private address, or touched the other destination")
	}
}

func TestProgramLostAndBack(t *testing.T) {
	t.Parallel()
	e := liveEnv(t)
	e.Processes.Last(t, "b1", "").Exit("Failed reading RTSP data: End of file")
	o := e.Output(t, "b1")
	if o.State != broadcast.Interrupted || !o.Deadline.Equal(e.Clock.Now().Add(outputtest.Grace)) || e.OnAir(t, "b1") {
		t.Fatalf("after the loss: %+v on air %v", o, e.OnAir(t, "b1"))
	}
	if !e.Processes.Last(t, "b1", "dA").Stopped() || e.Status("b1", "dA").State != destination.Off {
		t.Fatal("the relays did not stop")
	}
	e.Tick(time.Second)
	e.Processes.Last(t, "b1", "").Flow()
	if o := e.Output(t, "b1"); o.State != broadcast.Live || !o.Deadline.IsZero() || !e.OnAir(t, "b1") {
		t.Fatalf("after the return: %+v", o)
	}
	e.Processes.Last(t, "b1", "dA").Flow()
	if e.Status("b1", "dA").State != destination.Live {
		t.Fatal("dA is not live again")
	}
}

func TestDeadlinePassed(t *testing.T) {
	t.Parallel()
	e := liveEnv(t)
	if _, _, err := e.Service.Publish(ctx, "u1", "b1", []byte("offer")); err != nil {
		t.Fatal(err)
	}
	e.Processes.Last(t, "b1", "").Exit("lost")
	e.Tick(outputtest.Grace - time.Second)
	if e.Output(t, "b1").State != broadcast.Interrupted {
		t.Fatal("ended before the deadline")
	}
	e.Tick(time.Second)
	if o := e.Output(t, "b1"); o.State != broadcast.Offline || o.Reason != output.ReasonProgramLost || o.IngestURL != "" {
		t.Fatalf("%+v", o)
	}
	for _, p := range append(e.Processes.Started("b1", ""), e.Processes.Started("b1", "dA")...) {
		if !p.Stopped() {
			t.Fatal("a process of the broadcast is left")
		}
	}
	eventually(t, "the ingest session to close", func() bool { return e.Ingest.PublisherOf("b1") == "" })
}

func TestNoProgram(t *testing.T) {
	t.Parallel()
	e := outputtest.New(t)
	e.AddDestination(t, "dA", "u1", "A", "")
	e.AddBroadcast(t, "b1", "u1", "dA")
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	for range 29 {
		e.Processes.Last(t, "b1", "").Exit("404 Not Found")
		e.Tick(time.Second)
	}
	if e.Output(t, "b1").State != broadcast.Starting || len(e.Processes.Started("b1", "")) != 30 {
		t.Fatalf("the transcoder was not retried every second: %d", len(e.Processes.Started("b1", "")))
	}
	e.Tick(time.Second)
	if o := e.Output(t, "b1"); o.State != broadcast.Offline || o.Reason != output.ReasonNoProgram {
		t.Fatalf("%+v", o)
	}
	if !e.Processes.Last(t, "b1", "").Stopped() {
		t.Fatal("the transcoder is left running")
	}
	// capacity and claims were released
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
}

func TestEnd(t *testing.T) {
	t.Parallel()
	e := liveEnv(t)
	_, sid, err := e.Service.Publish(ctx, "u1", "b1", []byte("offer"))
	if err != nil || sid == "" {
		t.Fatal(err)
	}
	if _, err := e.Service.End(ctx, "u2", "b1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another user ended it: %v", err)
	}
	o, err := e.Service.End(ctx, "u1", "b1")
	if err != nil || o.State != broadcast.Offline || o.Reason != "" {
		t.Fatalf("%+v %v", o, err)
	}
	if e.OnAir(t, "b1") || e.Output(t, "b1").IngestID != "" || e.Ingest.PublisherOf("b1") != "" {
		t.Fatal("on air, or the ingest session is left")
	}
	for _, d := range []string{"", "dA", "dB"} {
		if !e.Processes.Last(t, "b1", d).Stopped() {
			t.Fatalf("process %q left running", d)
		}
	}
	if _, err := e.Service.End(ctx, "u1", "b1"); err != nil {
		t.Fatal("ending an offline broadcast:", err)
	}

	// end then go live again: late events of the first run change nothing
	old := e.Processes.Last(t, "b1", "")
	oldRelay := e.Processes.Last(t, "b1", "dA")
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	old.Flow()
	oldRelay.Exit("late")
	old.Exit("late")
	if o := e.Output(t, "b1"); o.State != broadcast.Starting || o.Run != 2 {
		t.Fatalf("a late event acted: %+v", o)
	}
	e.Tick(time.Second)
	if n := len(e.Processes.Started("b1", "")); n != 2 {
		t.Fatalf("%d transcoders", n)
	}
}

func TestEndWhileStarting(t *testing.T) {
	t.Parallel()
	e := outputtest.New(t)
	e.AddDestination(t, "dA", "u1", "A", "")
	e.AddBroadcast(t, "b1", "u1", "dA")
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	if o, err := e.Service.End(ctx, "u1", "b1"); err != nil || o.State != broadcast.Offline {
		t.Fatalf("%+v %v", o, err)
	}
	if !e.Processes.Last(t, "b1", "").Stopped() {
		t.Fatal("the transcoder is left")
	}
}

func TestRestartRecovery(t *testing.T) {
	t.Parallel()
	e := liveEnv(t)
	_, _, err := e.Service.Publish(ctx, "u1", "b1", []byte("offer"))
	if err != nil {
		t.Fatal(err)
	}
	// Run's recovery takes the live broadcast over (interrupted, transcoder restarted); it flows
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { e.Service.Run(runCtx); close(done) }()
	eventually(t, "the first recovery", func() bool { return len(e.Processes.Started("b1", "")) == 2 })
	e.Processes.Last(t, "b1", "").Flow()
	if e.Output(t, "b1").State != broadcast.Live {
		t.Fatal("not live")
	}

	// graceful stop: every process stops, nothing is stored as offline
	stop()
	<-done
	if e.Processes.StopAlls() != 1 {
		t.Fatal("the processes were not stopped")
	}
	e.Processes.Last(t, "b1", "").Exit("killed") // the processes' end after the stop changes nothing
	if e.Output(t, "b1").State != broadcast.Live {
		t.Fatalf("the stop changed the stored state: %+v", e.Output(t, "b1"))
	}

	// b2 was interrupted with 30 s left
	e.AddDestination(t, "dC", "u1", "C", "")
	e.AddBroadcast(t, "b2", "u1", "dC")
	deadline := e.Clock.Now().Add(30 * time.Second)
	_ = e.Broadcasts.SetOutput(ctx, "b2", broadcast.Output{State: broadcast.Interrupted, Run: 3, Deadline: deadline})

	// a new process on the same store
	ingestURL := e.Output(t, "b1").IngestURL
	e2 := &output.Service{ // the same ports, a fresh in-memory state
		Broadcasts: e.Broadcasts, Locks: &broadcast.Locks{}, Destinations: e.Dests, Targets: e.DestService,
		Stage: e.Service.Stage, Ingest: e.Ingest, Processes: e.Processes, MaxConcurrent: 2,
		StartTimeout: outputtest.StartTimeout, Grace: outputtest.Grace, Now: e.Clock.Now}
	runCtx, stop = context.WithCancel(ctx)
	defer stop()
	go e2.Run(runCtx)
	eventually(t, "recovery", func() bool { return len(e.Processes.Started("b2", "")) == 1 })
	if o := e.Output(t, "b1"); o.State != broadcast.Interrupted || o.Deadline.IsZero() {
		t.Fatalf("b1 after the restart: %+v", o)
	}
	if o := e.Output(t, "b2"); !o.Deadline.Equal(deadline) {
		t.Fatalf("b2's deadline moved: %+v", o)
	}
	// b1's owner is still publishing: it resumes as live
	e.Processes.Last(t, "b1", "").Flow()
	if e.Output(t, "b1").State != broadcast.Live {
		t.Fatal("b1 did not resume")
	}
	// b2's program never returns: offline at the original deadline
	e.Clock.Advance(30 * time.Second)
	eventually(t, "b2 to end", func() bool { return e.Output(t, "b2").State == broadcast.Offline })
	// ending b1 closes the ingest session opened before the restart
	if _, err := e2.End(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	closed, _, _ := e.Ingest.Snapshot()
	if !slices.Contains(closed, ingestURL) {
		t.Fatalf("closed %v, want %s", closed, ingestURL)
	}
}

func TestWHIP(t *testing.T) {
	t.Parallel()
	e := outputtest.New(t)
	e.AddDestination(t, "dA", "u1", "A", "")
	e.AddBroadcast(t, "b1", "u1", "dA")

	if _, _, err := e.Service.Publish(ctx, "u1", "b1", []byte("o")); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("offline broadcast: %v", err)
	}
	if _, err := e.Service.GoLive(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Service.Publish(ctx, "u2", "b1", []byte("o")); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("not the owner: %v", err)
	}
	if err := e.Service.Unpublish(ctx, "u2", "b1", "x"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("not the owner: %v", err)
	}
	if _, _, calls := e.Ingest.Snapshot(); calls != 0 {
		t.Fatal("a refused request reached the media server")
	}

	a1, sidA, err := e.Service.Publish(ctx, "u1", "b1", []byte("offer A"))
	if err != nil || string(a1.SDP) != "answer to offer A" || sidA == "" {
		t.Fatalf("%+v %v", a1, err)
	}
	_, sidB, err := e.Service.Publish(ctx, "u1", "b1", []byte("offer B"))
	if err != nil || sidB == sidA {
		t.Fatal(err)
	}
	closed, _, _ := e.Ingest.Snapshot()
	if !slices.Equal(closed, []string{a1.Resource}) {
		t.Fatalf("closed %v, want the first session", closed)
	}
	if err := e.Service.Unpublish(ctx, "u1", "b1", sidA); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("late DELETE of A: %v", err)
	}
	if e.Ingest.PublisherOf("b1") == "" || e.Output(t, "b1").IngestID != sidB {
		t.Fatal("B stopped publishing")
	}
	if err := e.Service.Unpublish(ctx, "u1", "b1", sidB); err != nil {
		t.Fatal(err)
	}
	if e.Ingest.PublisherOf("b1") != "" || e.Output(t, "b1").IngestURL != "" {
		t.Fatal("B is still publishing")
	}

	// media server error: 502, nothing stored, End at once
	e.Ingest.Hook = func(string) error { return errors.New("timeout") }
	if _, _, err := e.Service.Publish(ctx, "u1", "b1", []byte("o")); !errors.Is(err, output.ErrUpstream) {
		t.Fatalf("upstream error: %v", err)
	}
	if e.Output(t, "b1").IngestID != "" {
		t.Fatal("a session was stored")
	}

	// the run ends while the media server answers: the new session is closed, 409
	e.Ingest.Hook = func(id string) error {
		started := time.Now()
		if _, err := e.Service.End(ctx, "u1", id); err != nil || time.Since(started) > time.Second {
			t.Errorf("End waited for the media server: %v", err)
		}
		return nil
	}
	_, _, err = e.Service.Publish(ctx, "u1", "b1", []byte("o"))
	conflictMessage(t, err)
	if e.Ingest.PublisherOf("b1") != "" || e.Output(t, "b1").IngestID != "" {
		t.Fatal("the session of an ended run was kept")
	}
}

func TestCloseOrphans(t *testing.T) {
	t.Parallel()
	e := liveEnv(t)
	e.AddBroadcast(t, "b2", "u1")
	e.Ingest.SetPublisher("b1", "live-session")
	e.Ingest.SetPublisher("b2", "orphan")
	e.Ingest.SetPublisher("gone", "orphan-of-a-deleted-broadcast")
	if err := e.Service.CloseOrphans(ctx); err != nil {
		t.Fatal(err)
	}
	_, kicked, _ := e.Ingest.Snapshot()
	slices.Sort(kicked)
	if !slices.Equal(kicked, []string{"orphan", "orphan-of-a-deleted-broadcast"}) || e.Ingest.PublisherOf("b1") == "" {
		t.Fatalf("kicked %v", kicked)
	}
}

func parallel(fns ...func() error) []error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, fn := range fns {
		wg.Go(func() {
			<-start
			errs[i] = fn()
		})
	}
	close(start)
	wg.Wait()
	return errs
}

func tally(t *testing.T, errs []error) (ok int, messages string) {
	t.Helper()
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		messages += conflictMessage(t, err) + "; "
	}
	return ok, messages
}
