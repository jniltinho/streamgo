package livekit_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/stage/stagetest"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/core/studio/studiotest"
	"github.com/jniltinho/streamgo/internal/livekit"
)

// The throwaway key of the LiveKit in deployments/compose/compose.test.yml.
const (
	testKey    = "streamgo-test"
	testSecret = "streamgo-test-livekit-secret-0123456789abcdef" //nolint:gosec // fixture of the throwaway test container
)

func server(t *testing.T) (*livekit.Rooms, string) {
	t.Helper()
	url := os.Getenv("STREAMGO_TEST_LIVEKIT_URL")
	if url == "" {
		t.Skip("STREAMGO_TEST_LIVEKIT_URL not set: the LiveKit integration test runs under make test-db")
	}
	r := livekit.New(url, testKey, testSecret)
	if err := r.Ready(context.Background()); err != nil {
		t.Fatalf("LiveKit at %s is not ready: %v", url, err)
	}
	return r, url
}

func roomName(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return stage.Room("IT" + strings.ToUpper(hex.EncodeToString(b)))
}

type client struct {
	room         *lksdk.Room
	disconnected chan lksdk.DisconnectionReason
	metadata     chan string
}

// connect joins as a browser would; the error is LiveKit refusing the join.
func connect(t *testing.T, url, token string) (*client, error) {
	t.Helper()
	c := &client{disconnected: make(chan lksdk.DisconnectionReason, 1), metadata: make(chan string, 8)}
	cb := lksdk.NewRoomCallback()
	cb.OnDisconnectedWithReason = func(r lksdk.DisconnectionReason) {
		select {
		case c.disconnected <- r:
		default:
		}
	}
	cb.OnRoomMetadataChanged = func(md string) { c.metadata <- md }
	room, err := lksdk.ConnectToRoomWithToken("ws"+strings.TrimPrefix(url, "http"), token, cb,
		lksdk.WithAutoSubscribe(false), lksdk.WithConnectTimeout(10*time.Second))
	if err != nil {
		return nil, err
	}
	c.room = room
	t.Cleanup(room.Disconnect)
	return c, nil
}

func (c *client) waitDisconnected(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case <-c.disconnected:
	case <-time.After(within):
		t.Fatalf("still connected after %v", within)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func participants(t *testing.T, r *livekit.Rooms, room string) []string {
	t.Helper()
	ps, err := r.ListParticipants(context.Background(), room)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.Identity)
	}
	return ids
}

func metadataOf(t *testing.T, r *livekit.Rooms, room string) (string, bool) {
	t.Helper()
	rooms, err := r.ListRooms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, rm := range rooms {
		if rm.Name == room {
			return rm.Metadata, true
		}
	}
	return "", false
}

// Requirements "Owner joins the studio" and "Rooms belong to the server" against a real
// LiveKit with automatic room creation off.
func TestRoomsAgainstLiveKit(t *testing.T) {
	r, url := server(t)
	ctx := context.Background()
	room := roomName(t)
	t.Cleanup(func() { _ = r.DeleteRoom(context.Background(), room) })

	// a valid token cannot create the room
	tok, err := r.Token(room, "host-u1", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connect(t, url, tok); err == nil {
		t.Fatal("a token created a room")
	}
	if _, ok := metadataOf(t, r, room); ok {
		t.Fatal("the refused join left a room")
	}
	if err := r.UpdateMetadata(ctx, room, "{}"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("metadata of a missing room: %v", err)
	}
	if ps, err := r.ListParticipants(ctx, room); err != nil || len(ps) != 0 {
		t.Fatalf("participants of a missing room: %v, %v", ps, err)
	}

	// the server creates it with the stored state; joining reads it
	st := stage.Default()
	st.Layout = "spotlight"
	if err := r.CreateRoom(ctx, room, st.JSON(), 2); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateRoom(ctx, room, "ignored", 2); err != nil { // already open: unchanged
		t.Fatal(err)
	}
	if md, ok := metadataOf(t, r, room); !ok || md != st.JSON() {
		t.Fatalf("metadata %q", md)
	}
	owner, err := connect(t, url, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got := owner.room.Metadata(); got != st.JSON() {
		t.Fatalf("joined with metadata %q", got)
	}

	// a participant's token cannot change the room's metadata
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url+"/twirp/livekit.RoomService/UpdateRoomMetadata",
		strings.NewReader(`{"room":"`+room+`","metadata":"{\"v\":99}"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode < 400 {
		t.Fatalf("a participant token updated the metadata: %d", res.StatusCode)
	}

	// the server's push reaches the participants
	st.V = 2
	if err := r.UpdateMetadata(ctx, room, st.JSON()); err != nil {
		t.Fatal(err)
	}
	select {
	case md := <-owner.metadata:
		if md != st.JSON() {
			t.Fatalf("pushed %q", md)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the push did not reach the participant")
	}

	// capacity is LiveKit's: a third participant in a room of two is refused
	gtok, _ := r.Token(room, "guest-1", "Guest")
	guest, err := connect(t, url, gtok)
	if err != nil {
		t.Fatal(err)
	}
	extra, _ := r.Token(room, "guest-2", "Late")
	if _, err := connect(t, url, extra); err == nil {
		t.Fatal("a third participant joined a room of two")
	}
	eventually(t, "two participants", func() bool { return len(participants(t, r, room)) == 2 })

	// removing a participant disconnects them; removing again is not an error
	if err := r.RemoveParticipant(ctx, room, "guest-1"); err != nil {
		t.Fatal(err)
	}
	guest.waitDisconnected(t, 10*time.Second)
	if err := r.RemoveParticipant(ctx, room, "guest-1"); err != nil {
		t.Fatal(err)
	}

	// "Broadcast deleted with people in the room": the room closes and they are disconnected
	if err := r.DeleteRoom(ctx, room); err != nil {
		t.Fatal(err)
	}
	owner.waitDisconnected(t, 10*time.Second)
	if err := r.DeleteRoom(ctx, room); err != nil {
		t.Fatalf("deleting a closed room: %v", err)
	}
}

// Requirement "Room reconciliation" and the owner's priority, with the core on a real LiveKit.
func TestStudioAgainstLiveKit(t *testing.T) {
	r, url := server(t)
	ctx := context.Background()
	room := roomName(t)
	id := strings.TrimPrefix(room, stage.RoomPrefix)
	gone := roomName(t)
	t.Cleanup(func() {
		_ = r.DeleteRoom(context.Background(), room)
		_ = r.DeleteRoom(context.Background(), gone)
	})

	repo := &stagetest.Broadcasts{}
	repo.Put(broadcast.Broadcast{ID: id, OwnerID: "u1", Title: "T", InviteHash: studio.Hash("inv"), Stage: stage.Default().JSON(), StageVersion: 1})
	locks := &broadcast.Locks{}
	svc := &studio.Service{
		Broadcasts: repo, Locks: locks, Rooms: r, Guests: &studiotest.Guests{}, Banners: &studiotest.Banners{},
		Stage:     &stage.Writer{Broadcasts: repo, Locks: locks, Rooms: r, MaxOnStage: 6},
		OwnerName: func(context.Context, string) (string, error) { return "Owner", nil },
		URL:       url, MaxParticipants: 3, AssetsDir: t.TempDir(),
	}
	rec := &studio.Reconciler{Studio: svc}

	b, _ := svc.Invite(ctx, "inv")
	var guests []*client
	var identities []string
	for _, name := range []string{"Rafael", "Carla", "Bruno"} {
		j, err := svc.JoinAsGuest(ctx, b, name, "")
		if err != nil {
			t.Fatal(err)
		}
		c, err := connect(t, url, j.Token)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		guests, identities = append(guests, c), append(identities, j.Identity)
		time.Sleep(20 * time.Millisecond) // distinct join times
	}

	// "Owner arrives at a full studio": the guest who joined last makes room
	j, err := svc.StudioToken(ctx, "u1", id)
	if err != nil {
		t.Fatal(err)
	}
	guests[2].waitDisconnected(t, 10*time.Second)
	eventually(t, "a free slot", func() bool { return len(participants(t, r, room)) == 2 })
	if _, err := connect(t, url, j.Token); err != nil {
		t.Fatalf("the owner could not connect: %v", err)
	}

	// a stage change whose push failed, a removed guest who reconnects, an unknown participant
	// and a room without a broadcast: one reconciler pass fixes them all
	if _, err := svc.RemoveGuest(ctx, "u1", id, identities[1]); err != nil {
		t.Fatal(err)
	}
	guests[1].waitDisconnected(t, 10*time.Second)
	stale := stage.Default()
	if err := r.UpdateMetadata(ctx, room, stale.JSON()); err != nil { // the room missed v2
		t.Fatal(err)
	}
	unknown, _ := r.Token(room, "intruder", "?")
	intruder, err := connect(t, url, unknown)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CreateRoom(ctx, gone, "", 2); err != nil {
		t.Fatal(err)
	}

	if err := rec.Once(ctx); err != nil {
		t.Fatal(err)
	}
	intruder.waitDisconnected(t, 10*time.Second)
	stored, _ := repo.Get(ctx, id)
	if md, _ := metadataOf(t, r, room); md != stored.Stage {
		t.Fatalf("room metadata %q, stored %q", md, stored.Stage)
	}
	if _, ok := metadataOf(t, r, gone); ok {
		t.Fatal("the room without a broadcast is still open")
	}
	eventually(t, "only the owner and Rafael", func() bool {
		ids := participants(t, r, room)
		return len(ids) == 2 && strings.Contains(strings.Join(ids, ","), identities[0]) && strings.Contains(strings.Join(ids, ","), "host-u1")
	})
}
