package studio_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/core/studio/studiotest"
)

var ctx = context.Background()

func ptr[T any](v T) *T { return &v }

func wantInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var inv *core.InvalidError
	if !errors.As(err, &inv) || inv.Field != field {
		t.Fatalf("got %v, want invalid %s", err, field)
	}
}

func onStage(e *studiotest.Env, t *testing.T, sources ...stage.Source) stage.State {
	t.Helper()
	s, err := e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{Sources: &sources})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOwnerToken(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	j, err := e.Service.StudioToken(ctx, "u1", "b1")
	if err != nil {
		t.Fatal(err)
	}
	if j.Identity != "host-u1" || j.URL != "wss://lk.example.test" || j.Token != "bc_b1|host-u1|Owner u1" {
		t.Fatalf("got %+v", j)
	}
	if _, err := e.Service.StudioToken(ctx, "u2", "b1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another user: %v", err)
	}
	if _, err := e.Service.StudioToken(ctx, "u1", "nope"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("missing broadcast: %v", err)
	}
}

// Scenarios "Room recreated with its state", "Joining reads the state" and "Reconnection
// after the room closed".
func TestRoomRecreatedWithItsState(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	if _, err := e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{Layout: ptr("spotlight")}); err != nil {
		t.Fatal(err) // "Room not open": stored and answered without a room
	}
	if e.Rooms.Get("bc_b1") != nil {
		t.Fatal("a patch opened the room")
	}
	if _, err := e.Service.StudioToken(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	if got := e.RoomStage(t, "b1"); got.Layout != "spotlight" || got.V != 2 {
		t.Fatalf("room metadata %+v", got)
	}

	// the room closed after staying empty: an old token cannot connect (auto_create off),
	// the studio asks for a new token, and the room comes back with the stored state
	_ = e.Rooms.DeleteRoom(ctx, "bc_b1")
	if err := e.Rooms.Connect("bc_b1", "host-u1", e.Clock.Now()); err == nil {
		t.Fatal("connected to a closed room")
	}
	if _, err := e.Service.StudioToken(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	if got := e.RoomStage(t, "b1"); got.Layout != "spotlight" {
		t.Fatalf("room metadata %+v", got)
	}

	// or a join recreated it (the race LiveKit allows) without metadata: the reconciler fixes it
	_ = e.Rooms.DeleteRoom(ctx, "bc_b1")
	_ = e.Rooms.CreateRoom(ctx, "bc_b1", "", 4)
	if err := e.Reconciler.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.RoomStage(t, "b1"); got.Layout != "spotlight" || got.V != 2 {
		t.Fatalf("room metadata after the reconciler %+v", got)
	}
}

// Scenario "Broadcast deleted with people in the room".
func TestTeardown(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	j := e.JoinGuest(t, "invite-b1", "Rafael", "")
	if err := e.Rooms.Connect("bc_b1", j.Identity, e.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	logo := uploadLogo(t, e)
	if err := e.Service.Teardown(ctx, "b1", logo); err != nil {
		t.Fatal(err)
	}
	if e.Rooms.Get("bc_b1") != nil {
		t.Fatal("the room is still open")
	}
	if _, err := os.Stat(e.Service.LogoPath(logo)); !os.IsNotExist(err) {
		t.Fatalf("the logo file is still there: %v", err)
	}
}

// Scenarios "Join backstage", "Invalid link", "Reload keeps the identity", "Two broadcasts".
func TestGuestJoin(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	e.AddBroadcast("b2", "u1", "invite-b2")

	rafael := e.JoinGuest(t, "invite-b1", "  Rafael Lima ", "")
	if rafael.Cookie == "" || rafael.Title != "Show b1" || rafael.Token != "bc_b1|"+rafael.Identity+"|Rafael Lima" {
		t.Fatalf("join %+v", rafael)
	}
	if len(e.Stored(t, "b1").Sources) != 0 {
		t.Fatal("a guest joined on stage")
	}
	if e.Rooms.Get("bc_b1") == nil {
		t.Fatal("joining did not open the room")
	}

	for _, tok := range []string{"", "nope", "invite-b1x"} {
		if _, err := e.Service.Invite(ctx, tok); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("invite %q: %v", tok, err)
		}
	}
	b, _ := e.Service.Invite(ctx, "invite-b1")
	for _, name := range []string{"", "   ", strings.Repeat("x", 41), "a\nb"} {
		if _, err := e.Service.JoinAsGuest(ctx, b, name, ""); err == nil {
			t.Fatalf("name %q accepted", name)
		} else {
			wantInvalid(t, err, "name")
		}
	}
	if _, err := e.Service.JoinAsGuest(ctx, b, strings.Repeat("é", 40), ""); err != nil {
		t.Fatalf("40 characters: %v", err)
	}

	// reload: the same cookie, the same identity, still on stage
	onStage(e, t, stage.Source{P: rafael.Identity, K: stage.Camera})
	e.Clock.Advance(time.Hour)
	again := e.JoinGuest(t, "invite-b1", "Rafael Lima", rafael.Cookie)
	if again.Identity != rafael.Identity || again.Cookie != rafael.Cookie {
		t.Fatalf("reload: %s, want %s", again.Identity, rafael.Identity)
	}
	if s := e.Stored(t, "b1"); len(s.Sources) != 1 || s.Sources[0].P != rafael.Identity {
		t.Fatalf("stage after reload %+v", s.Sources)
	}

	// another broadcast: its own guest; the first cookie means nothing there
	other := e.JoinGuest(t, "invite-b2", "Rafael Lima", rafael.Cookie)
	if other.Identity == rafael.Identity || other.Cookie == rafael.Cookie {
		t.Fatalf("b2 reused b1's guest: %+v", other)
	}
	if j := e.JoinGuest(t, "invite-b2", "Rafael Lima", other.Cookie); j.Identity != other.Identity {
		t.Fatal("b2 lost its identity")
	}
	if j := e.JoinGuest(t, "invite-b1", "Rafael Lima", rafael.Cookie); j.Identity != rafael.Identity {
		t.Fatal("b1 lost its identity")
	}
}

// Scenario "Expired guest".
func TestGuestExpires(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	j := e.JoinGuest(t, "invite-b1", "Rafael", "")
	e.Clock.Advance(studio.GuestLifetime + time.Second)
	again := e.JoinGuest(t, "invite-b1", "Rafael", j.Cookie)
	if again.Identity == j.Identity {
		t.Fatal("an expired guest came back")
	}
	if err := e.Reconciler.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if n := e.Guests.Len(); n != 1 {
		t.Fatalf("%d guests left, want only the new one", n)
	}
}

// Scenario "Too many guests".
func TestTooManyGuests(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	b, _ := e.Service.Invite(ctx, "invite-b1")
	var first *studio.GuestJoin
	for i := range studio.MaxGuests {
		j, err := e.Service.JoinAsGuest(ctx, b, fmt.Sprint("guest ", i), "")
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}
		if i == 0 {
			first = j
		}
	}
	if _, err := e.Service.JoinAsGuest(ctx, b, "one too many", ""); !errors.Is(err, studio.ErrFull) {
		t.Fatalf("201st guest: %v", err)
	}
	if n := e.Guests.Len(); n != studio.MaxGuests {
		t.Fatalf("%d guests", n)
	}
	// an existing guest still gets back in
	if j, err := e.Service.JoinAsGuest(ctx, b, "guest 0", first.Cookie); err != nil || j.Identity != first.Identity {
		t.Fatalf("existing guest: %v", err)
	}
	// expired guests do not count
	e.Clock.Advance(studio.GuestLifetime + time.Second)
	if _, err := e.Service.JoinAsGuest(ctx, b, "later", ""); err != nil {
		t.Fatal(err)
	}
}

// Scenarios "Studio full", "Owner arrives at a full studio", "Slot taken before the owner connects".
func TestCapacity(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t) // 4 participants, the owner included
	var ids, cookies []string
	for i := range e.MaxInStudio {
		j := e.JoinGuest(t, "invite-b1", fmt.Sprint("guest ", i), "")
		cookies = append(cookies, j.Cookie)
		e.Clock.Advance(time.Second)
		if err := e.Rooms.Connect("bc_b1", j.Identity, e.Clock.Now()); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.Identity)
	}
	b, _ := e.Service.Invite(ctx, "invite-b1")
	guests := e.Guests.Len()
	if _, err := e.Service.JoinAsGuest(ctx, b, "late", ""); !errors.Is(err, studio.ErrRoomFull) || e.Guests.Len() != guests {
		t.Fatalf("a fifth participant: %v, %d guests", err, e.Guests.Len())
	}
	// a guest in the room reloads the page: the same identity, not a refusal
	if j, err := e.Service.JoinAsGuest(ctx, b, "guest 0", cookies[0]); err != nil || j.Identity != ids[0] {
		t.Fatalf("reload in a full room: %v", err)
	}

	// the owner arrives: the guest who joined last makes room
	if _, err := e.Service.StudioToken(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	if got := e.Rooms.Removed; len(got) != 1 || got[0] != ids[3] {
		t.Fatalf("removed %v, want %s", got, ids[3])
	}
	// a guest takes the slot first; the studio retries with a new token
	e.Clock.Advance(time.Second)
	extra := e.JoinGuest(t, "invite-b1", "late", "")
	if err := e.Rooms.Connect("bc_b1", extra.Identity, e.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	connected := false
	for attempt := 0; attempt < 3 && !connected; attempt++ {
		if _, err := e.Service.StudioToken(ctx, "u1", "b1"); err != nil {
			t.Fatal(err)
		}
		connected = e.Rooms.Connect("bc_b1", "host-u1", e.Clock.Now()) == nil
	}
	if !connected {
		t.Fatal("the owner did not connect within three attempts")
	}
	if got := e.Rooms.Removed; got[len(got)-1] != extra.Identity {
		t.Fatalf("removed %v, want %s last", got, extra.Identity)
	}
	// with the owner in, a new token removes nobody
	n := len(e.Rooms.Removed)
	if _, err := e.Service.StudioToken(ctx, "u1", "b1"); err != nil || len(e.Rooms.Removed) != n {
		t.Fatalf("owner present: removed %v (%v)", e.Rooms.Removed, err)
	}
}

// Scenarios "Removed guest is taken off stage", "Removal reaches the program even when the
// room is not updated", "Reconnection with an old token", "Old cookie after removal".
func TestRemoveGuest(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	carla := e.JoinGuest(t, "invite-b1", "Carla", "")
	rafael := e.JoinGuest(t, "invite-b1", "Rafael", "")
	for _, id := range []string{carla.Identity, rafael.Identity} {
		if err := e.Rooms.Connect("bc_b1", id, e.Clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	before := onStage(e, t, stage.Source{P: carla.Identity, K: stage.Camera},
		stage.Source{P: carla.Identity, K: stage.Screen}, stage.Source{P: rafael.Identity, K: stage.Camera})

	e.Rooms.FailPush = true
	s, err := e.Service.RemoveGuest(ctx, "u1", "b1", carla.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if s.V != before.V+1 || len(s.Sources) != 1 || s.Sources[0].P != rafael.Identity {
		t.Fatalf("answered stage %+v", s)
	}
	if got := e.Stored(t, "b1"); got.JSON() != s.JSON() {
		t.Fatalf("stored %s", got.JSON())
	}
	if p := e.Rooms.Get("bc_b1").Participants; len(p) != 1 || p[0].Identity != rafael.Identity {
		t.Fatalf("participants %v", p)
	}

	// she reconnects with her old token; the reconciler removes her and repairs the metadata
	e.Rooms.FailPush = false
	if err := e.Rooms.Connect("bc_b1", carla.Identity, e.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := e.Reconciler.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if p := e.Rooms.Get("bc_b1").Participants; len(p) != 1 {
		t.Fatalf("participants after the reconciler %v", p)
	}
	if got := e.RoomStage(t, "b1"); got.V != s.V || strings.Contains(got.JSON(), carla.Identity) {
		t.Fatalf("room stage %s", got.JSON())
	}
	// she cannot be put back on stage
	_, err = e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{Sources: &[]stage.Source{{P: carla.Identity, K: stage.Camera}}})
	wantInvalid(t, err, "sources")

	// the old cookie gives a new identity, backstage, while the link is valid
	back := e.JoinGuest(t, "invite-b1", "Carla", carla.Cookie)
	if back.Identity == carla.Identity || back.Cookie == carla.Cookie {
		t.Fatal("the removed identity came back")
	}
	if strings.Contains(e.Stored(t, "b1").JSON(), back.Identity) {
		t.Fatal("rejoined on stage")
	}
	e.Broadcasts.Put(func() broadcast.Broadcast { // the owner rotates the link
		b, _ := e.Broadcasts.Get(ctx, "b1")
		b.InviteHash = studio.Hash("rotated")
		return *b
	}())
	if _, err := e.Service.Invite(ctx, "invite-b1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("rotated link: %v", err)
	}

	// the owner cannot be removed; unknown guests and other owners get 404
	_, err = e.Service.RemoveGuest(ctx, "u1", "b1", "host-u1")
	wantInvalid(t, err, "identity")
	if _, err := e.Service.RemoveGuest(ctx, "u1", "b1", "guest-nobody"); !errors.Is(err, core.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := e.Service.RemoveGuest(ctx, "u2", "b1", rafael.Identity); !errors.Is(err, core.ErrNotFound) {
		t.Fatal(err)
	}
}

// Requirement "Room reconciliation": "Missed update", "Unknown participant", "After a server
// restart", and a room without a broadcast.
func TestReconciler(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	e.Service.MaxParticipants = 10
	if _, err := e.Service.StudioToken(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	carla := e.JoinGuest(t, "invite-b1", "Carla", "")
	e.Rooms.FailPush = true
	s, err := e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{Layout: ptr("pip")})
	if err != nil {
		t.Fatal(err)
	}
	e.Rooms.FailPush = false
	for _, id := range []string{"host-u1", carla.Identity, "intruder", "guest-NOPE", "host-u2"} {
		if err := e.Rooms.Connect("bc_b1", id, e.Clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	_ = e.Rooms.CreateRoom(ctx, "bc_gone", `{"v":3}`, 4)
	_ = e.Rooms.CreateRoom(ctx, "meeting-x", "", 4) // not a studio room: left alone
	if _, err := e.Service.RemoveGuest(ctx, "u1", "b1", carla.Identity); err != nil {
		t.Fatal(err)
	}
	// the server restarts before she reconnects: a new service on the same database
	restarted := studiotest.New(t)
	restarted.Service.Broadcasts, restarted.Service.Guests = e.Broadcasts, e.Guests
	restarted.Service.Rooms, restarted.Service.Stage.Rooms = e.Rooms, e.Rooms
	if err := e.Rooms.Connect("bc_b1", carla.Identity, e.Clock.Now()); err != nil {
		t.Fatal(err)
	}

	ran := 0
	restarted.Reconciler.Steps = []studio.Step{func(context.Context) error { ran++; return nil }}
	if err := restarted.Reconciler.Once(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.RoomStage(t, "b1"); got.V != s.V+1 || got.Layout != "pip" {
		t.Fatalf("room stage %+v, want v%d", got, s.V+1)
	}
	var left []string
	for _, p := range e.Rooms.Get("bc_b1").Participants {
		left = append(left, p.Identity)
	}
	if strings.Join(left, ",") != "host-u1" {
		t.Fatalf("participants %v", left)
	}
	if e.Rooms.Get("bc_gone") != nil || e.Rooms.Get("meeting-x") == nil {
		t.Fatal("rooms not reconciled")
	}
	if ran != 1 {
		t.Fatalf("extra step ran %d times", ran)
	}
}

func TestRunReconcilesAtStartupAndStops(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	ran := make(chan struct{}, 1)
	e.Reconciler.Steps = []studio.Step{func(context.Context) error {
		select {
		case ran <- struct{}{}:
		default:
		}
		return errors.New("a failed step is logged, not fatal")
	}}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { e.Reconciler.Run(runCtx); close(done) }()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("no pass at startup")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

// Requirement "Changing the stage".
func TestPatchStage(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	e.AddBroadcast("b2", "u2", "invite-b2")
	if _, err := e.Service.StudioToken(ctx, "u1", "b1"); err != nil {
		t.Fatal(err)
	}
	rafael := e.JoinGuest(t, "invite-b1", "Rafael", "")
	other := e.JoinGuest(t, "invite-b2", "Someone", "")
	bn, err := e.Service.AddBanner(ctx, "u1", "b1", "Q3 roadmap")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.Service.AddBanner(ctx, "u2", "b2", "not yours")
	if err != nil {
		t.Fatal(err)
	}

	// put a guest on stage
	s := onStage(e, t, stage.Source{P: "host-u1", K: stage.Camera}, stage.Source{P: rafael.Identity, K: stage.Camera})
	if s.V != 2 || e.RoomStage(t, "b1").JSON() != s.JSON() || e.Stored(t, "b1").JSON() != s.JSON() {
		t.Fatalf("stage %s, room %s", s.JSON(), e.RoomStage(t, "b1").JSON())
	}

	src := func(p, k string) *[]stage.Source { return &[]stage.Source{{P: p, K: k}} }
	invalid := []struct {
		name  string
		patch studio.Patch
		field string
	}{
		{"too many sources", studio.Patch{Sources: &[]stage.Source{
			{P: "host-u1", K: "camera"}, {P: "host-u1", K: "screen"}, {P: rafael.Identity, K: "camera"}, {P: rafael.Identity, K: "screen"},
		}}, "sources"},
		{"unknown layout", studio.Patch{Layout: ptr("mosaic")}, "layout"},
		{"duplicate source", studio.Patch{Sources: &[]stage.Source{{P: "host-u1", K: "camera"}, {P: "host-u1", K: "camera"}}}, "sources"},
		{"guest of another broadcast", studio.Patch{Sources: src(other.Identity, "camera")}, "sources"},
		{"another owner", studio.Patch{Sources: src("host-u2", "camera")}, "sources"},
		{"unknown kind", studio.Patch{Sources: src("host-u1", "mic")}, "sources"},
		{"banner of another broadcast", studio.Patch{BannerSet: true, BannerID: &foreign.ID}, "banner_id"},
		{"accent", studio.Patch{Accent: ptr("#12345g")}, "accent"},
	}
	for _, tt := range invalid {
		_, err := e.Service.PatchStage(ctx, "u1", "b1", tt.patch)
		wantInvalid(t, err, tt.field)
		if got := e.Stored(t, "b1"); got.JSON() != s.JSON() {
			t.Fatalf("%s changed the state: %s", tt.name, got.JSON())
		}
	}
	// a guest or another user cannot change it
	if _, err := e.Service.PatchStage(ctx, "u2", "b1", studio.Patch{Layout: ptr("solo")}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("another user: %v", err)
	}

	// show a banner, then hide it
	s, err = e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{BannerSet: true, BannerID: &bn.ID, Accent: ptr("#AABBCC"), Layout: ptr("solo")})
	if err != nil || s.Banner == nil || *s.Banner != (stage.Banner{ID: bn.ID, Text: "Q3 roadmap"}) || s.Accent != "#AABBCC" || s.Layout != "solo" {
		t.Fatalf("banner shown: %+v, %v", s, err)
	}
	s, err = e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{BannerSet: true})
	if err != nil || s.Banner != nil || len(s.Sources) != 2 {
		t.Fatalf("banner hidden: %+v, %v", s, err)
	}
}

// Scenario "Concurrent changes".
func TestConcurrentPatchAndRemoval(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	carla := e.JoinGuest(t, "invite-b1", "Carla", "")
	before := onStage(e, t, stage.Source{P: carla.Identity, K: stage.Camera})
	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{Layout: ptr("solo")}); err != nil {
			t.Error(err)
		}
	})
	wg.Go(func() {
		if _, err := e.Service.RemoveGuest(ctx, "u1", "b1", carla.Identity); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	got := e.Stored(t, "b1")
	if got.V != before.V+2 || got.Layout != "solo" || len(got.Sources) != 0 {
		t.Fatalf("stored %s", got.JSON())
	}
}

// Requirement "Banners".
func TestBanners(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	for _, txt := range []string{"", strings.Repeat("b", 121)} {
		_, err := e.Service.AddBanner(ctx, "u1", "b1", txt)
		wantInvalid(t, err, "text")
	}
	var ids []string
	for i := range studio.MaxBanners {
		bn, err := e.Service.AddBanner(ctx, "u1", "b1", fmt.Sprint("banner ", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, bn.ID)
	}
	_, err := e.Service.AddBanner(ctx, "u1", "b1", "51st")
	wantInvalid(t, err, "text")
	if list, err := e.Service.ListBanners(ctx, "u1", "b1"); err != nil || len(list) != studio.MaxBanners {
		t.Fatalf("%d banners, %v", len(list), err)
	}
	if _, err := e.Service.ListBanners(ctx, "u2", "b1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatal(err)
	}

	if _, err := e.Service.PatchStage(ctx, "u1", "b1", studio.Patch{BannerSet: true, BannerID: &ids[0]}); err != nil {
		t.Fatal(err)
	}
	// deleting another banner keeps the shown one; deleting the shown one hides it
	if s, err := e.Service.DeleteBanner(ctx, "u1", "b1", ids[1]); err != nil || s.Banner == nil {
		t.Fatalf("%+v, %v", s, err)
	}
	if s, err := e.Service.DeleteBanner(ctx, "u1", "b1", ids[0]); err != nil || s.Banner != nil || e.Stored(t, "b1").Banner != nil {
		t.Fatalf("%+v, %v", s, err)
	}
	if _, err := e.Service.DeleteBanner(ctx, "u1", "b1", ids[0]); !errors.Is(err, core.ErrNotFound) {
		t.Fatal(err)
	}
}

// Requirement "Logo": upload, replace, failure keeps the previous one, delete.
func TestLogo(t *testing.T) {
	t.Parallel()
	e := studiotest.New(t)
	first := uploadLogo(t, e)
	if data, err := os.ReadFile(e.Service.LogoPath(first)); err != nil || string(data) != "fake png bytes" {
		t.Fatalf("stored %q, %v", data, err)
	}
	b, _ := e.Broadcasts.Get(ctx, "b1")
	if b.Logo != first {
		t.Fatalf("recorded %q", b.Logo)
	}
	second := uploadLogo(t, e)
	if _, err := os.Stat(e.Service.LogoPath(first)); !os.IsNotExist(err) {
		t.Fatal("the first file is still there")
	}

	// a failed upload keeps the previous logo
	_, err := e.Service.SetLogo(ctx, "u1", "b1", []byte("<svg/>"), ".svg")
	wantInvalid(t, err, "file")
	if _, err := e.Service.SetLogo(ctx, "u2", "b1", []byte("x"), ".png"); !errors.Is(err, core.ErrNotFound) {
		t.Fatal(err)
	}
	if s := e.Stored(t, "b1"); s.Logo == nil || *s.Logo != studio.LogoURLPrefix+second {
		t.Fatalf("logo %v", s.Logo)
	}
	entries, _ := os.ReadDir(e.Service.AssetsDir)
	if len(entries) != 1 {
		t.Fatalf("%d files in assets", len(entries))
	}

	s, err := e.Service.DeleteLogo(ctx, "u1", "b1")
	if err != nil || s.Logo != nil {
		t.Fatalf("%+v, %v", s, err)
	}
	if _, err := os.Stat(e.Service.LogoPath(second)); !os.IsNotExist(err) {
		t.Fatal("the deleted logo's file is still there")
	}
	for _, name := range []string{"", "../x.png", "abc.png", strings.Repeat("a", 32) + ".svg"} {
		if e.Service.LogoPath(name) != "" {
			t.Fatalf("LogoPath(%q) accepted", name)
		}
	}
}

func uploadLogo(t *testing.T, e *studiotest.Env) string {
	t.Helper()
	s, err := e.Service.SetLogo(ctx, "u1", "b1", []byte("fake png bytes"), ".png")
	if err != nil {
		t.Fatal(err)
	}
	if s.Logo == nil || !strings.HasPrefix(*s.Logo, studio.LogoURLPrefix) {
		t.Fatalf("logo %v", s.Logo)
	}
	return strings.TrimPrefix(*s.Logo, studio.LogoURLPrefix)
}
