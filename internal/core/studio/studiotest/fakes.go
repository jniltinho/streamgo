// Package studiotest holds in-memory fakes of the studio's ports (LiveKit rooms, guests,
// banners) and a service wired to them, for the core's and the handlers' tests.
package studiotest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/stage/stagetest"
	"github.com/jniltinho/streamgo/internal/core/studio"
)

// Room is a fake LiveKit room.
type Room struct {
	Metadata     string
	Max          int
	Participants []studio.Participant
}

// Rooms is a fake LiveKit with automatic room creation off.
type Rooms struct {
	mu        sync.Mutex
	rooms     map[string]*Room
	FailPush  bool // UpdateMetadata fails, as when LiveKit is unreachable
	Removed   []string
	TokenLog  []string
	CreateLog []string
}

// Token returns a readable fake token.
func (f *Rooms) Token(room, identity, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := room + "|" + identity + "|" + name
	f.TokenLog = append(f.TokenLog, t)
	return t, nil
}

// CreateRoom opens room unless it exists.
func (f *Rooms) CreateRoom(_ context.Context, room, md string, maxParticipants int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateLog = append(f.CreateLog, room)
	if f.rooms == nil {
		f.rooms = map[string]*Room{}
	}
	if f.rooms[room] == nil {
		f.rooms[room] = &Room{Metadata: md, Max: maxParticipants}
	}
	return nil
}

// UpdateMetadata replaces the room's metadata.
func (f *Rooms) UpdateMetadata(_ context.Context, room, md string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FailPush {
		return fmt.Errorf("livekit unreachable")
	}
	r := f.rooms[room]
	if r == nil {
		return fmt.Errorf("room %s: %w", room, core.ErrNotFound)
	}
	r.Metadata = md
	return nil
}

// ListRooms lists the open rooms.
func (f *Rooms) ListRooms(context.Context) ([]studio.Room, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []studio.Room
	for name, r := range f.rooms {
		out = append(out, studio.Room{Name: name, Metadata: r.Metadata})
	}
	return out, nil
}

// ListParticipants lists who is in room.
func (f *Rooms) ListParticipants(_ context.Context, room string) ([]studio.Participant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.rooms[room]; r != nil {
		return slices.Clone(r.Participants), nil
	}
	return nil, nil
}

// RemoveParticipant disconnects identity.
func (f *Rooms) RemoveParticipant(_ context.Context, room, identity string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Removed = append(f.Removed, identity)
	if r := f.rooms[room]; r != nil {
		r.Participants = slices.DeleteFunc(r.Participants, func(p studio.Participant) bool { return p.Identity == identity })
	}
	return nil
}

// DeleteRoom closes room.
func (f *Rooms) DeleteRoom(_ context.Context, room string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rooms, room)
	return nil
}

// Connect is a client joining with a token: it fails when the room does not exist or is full
// (LiveKit with auto_create off), and replaces an older connection of the same identity.
func (f *Rooms) Connect(room, identity string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rooms[room]
	if r == nil {
		return fmt.Errorf("room %s does not exist", room)
	}
	r.Participants = slices.DeleteFunc(r.Participants, func(p studio.Participant) bool { return p.Identity == identity })
	if len(r.Participants) >= r.Max {
		return fmt.Errorf("room %s is full", room)
	}
	r.Participants = append(r.Participants, studio.Participant{Identity: identity, JoinedAt: at})
	return nil
}

// Get returns a copy of room, or nil.
func (f *Rooms) Get(room string) *Room {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.rooms[room]; r != nil {
		c := *r
		c.Participants = slices.Clone(r.Participants)
		return &c
	}
	return nil
}

// Guests is an in-memory studio.GuestRepository.
type Guests struct {
	mu   sync.Mutex
	rows []studio.Guest
	seq  int
}

// Create stores g with the next id.
func (f *Guests) Create(_ context.Context, g *studio.Guest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	g.ID = fmt.Sprintf("G%04d", f.seq)
	f.rows = append(f.rows, *g)
	return nil
}

func (f *Guests) find(match func(*studio.Guest) bool) (*studio.Guest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if match(&f.rows[i]) {
			g := f.rows[i]
			return &g, nil
		}
	}
	return nil, fmt.Errorf("guest: %w", core.ErrNotFound)
}

// Get finds a guest of the broadcast.
func (f *Guests) Get(_ context.Context, broadcastID, id string) (*studio.Guest, error) {
	return f.find(func(g *studio.Guest) bool { return g.BroadcastID == broadcastID && g.ID == id })
}

// FindByCookie finds a guest of the broadcast by cookie hash.
func (f *Guests) FindByCookie(_ context.Context, broadcastID, hash string) (*studio.Guest, error) {
	return f.find(func(g *studio.Guest) bool { return g.BroadcastID == broadcastID && g.CookieHash == hash })
}

func (f *Guests) update(id string, fn func(*studio.Guest)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id {
			fn(&f.rows[i])
			return nil
		}
	}
	return fmt.Errorf("guest: %w", core.ErrNotFound)
}

// Rejoin sets the name and last join.
func (f *Guests) Rejoin(_ context.Context, id, name string, at time.Time) error {
	return f.update(id, func(g *studio.Guest) { g.Name, g.LastJoinAt = name, at })
}

// MarkRemoved marks the guest removed.
func (f *Guests) MarkRemoved(_ context.Context, id string, at time.Time) error {
	return f.update(id, func(g *studio.Guest) { g.RemovedAt = at })
}

// CountSince counts the broadcast's guests whose last join is after since.
func (f *Guests) CountSince(_ context.Context, broadcastID string, since time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, g := range f.rows {
		if g.BroadcastID == broadcastID && g.LastJoinAt.After(since) {
			n++
		}
	}
	return n, nil
}

// DeleteBefore deletes guests whose last join is before t.
func (f *Guests) DeleteBefore(_ context.Context, t time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	before := len(f.rows)
	f.rows = slices.DeleteFunc(f.rows, func(g studio.Guest) bool { return g.LastJoinAt.Before(t) })
	return int64(before - len(f.rows)), nil
}

// Len is the number of stored guests.
func (f *Guests) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// Banners is an in-memory studio.BannerRepository.
type Banners struct {
	mu   sync.Mutex
	rows []studio.Banner
	seq  int
}

// Create stores bn with the next id.
func (f *Banners) Create(_ context.Context, bn *studio.Banner) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	bn.ID = fmt.Sprintf("N%04d", f.seq)
	f.rows = append(f.rows, *bn)
	return nil
}

// Get finds a banner of the broadcast.
func (f *Banners) Get(_ context.Context, broadcastID, id string) (*studio.Banner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, bn := range f.rows {
		if bn.BroadcastID == broadcastID && bn.ID == id {
			return &bn, nil
		}
	}
	return nil, fmt.Errorf("banner: %w", core.ErrNotFound)
}

// List lists the broadcast's banners in creation order.
func (f *Banners) List(_ context.Context, broadcastID string) ([]studio.Banner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []studio.Banner{}
	for _, bn := range f.rows {
		if bn.BroadcastID == broadcastID {
			out = append(out, bn)
		}
	}
	return out, nil
}

// Delete removes a banner of the broadcast.
func (f *Banners) Delete(_ context.Context, broadcastID, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.rows)
	f.rows = slices.DeleteFunc(f.rows, func(bn studio.Banner) bool { return bn.BroadcastID == broadcastID && bn.ID == id })
	if len(f.rows) == n {
		return fmt.Errorf("banner: %w", core.ErrNotFound)
	}
	return nil
}

// Clock is a controlled clock.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Env is a studio service on fakes, with the broadcast "b1" owned by "u1" whose invite token
// is "invite-b1".
type Env struct {
	Service     *studio.Service
	Reconciler  *studio.Reconciler
	Broadcasts  *stagetest.Broadcasts
	Rooms       *Rooms
	Guests      *Guests
	Banners     *Banners
	Clock       *Clock
	Locks       *broadcast.Locks
	MaxOnStage  int
	MaxInStudio int
}

// New builds an Env with room for 4 participants and 3 sources on stage.
func New(t *testing.T) *Env {
	t.Helper()
	e := &Env{
		Broadcasts: &stagetest.Broadcasts{}, Rooms: &Rooms{}, Guests: &Guests{}, Banners: &Banners{},
		Clock: &Clock{t: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}, Locks: &broadcast.Locks{},
		MaxOnStage: 3, MaxInStudio: 4,
	}
	e.AddBroadcast("b1", "u1", "invite-b1")
	w := &stage.Writer{Broadcasts: e.Broadcasts, Locks: e.Locks, Rooms: e.Rooms, MaxOnStage: e.MaxOnStage}
	e.Service = &studio.Service{
		Broadcasts: e.Broadcasts, Locks: e.Locks, Stage: w, Rooms: e.Rooms, Guests: e.Guests, Banners: e.Banners,
		OwnerName: func(_ context.Context, id string) (string, error) { return "Owner " + id, nil },
		URL:       "wss://lk.example.test", MaxParticipants: e.MaxInStudio, AssetsDir: t.TempDir(), Now: e.Clock.Now,
	}
	e.Reconciler = &studio.Reconciler{Studio: e.Service}
	return e
}

// AddBroadcast stores a broadcast with the default stage.
func (e *Env) AddBroadcast(id, owner, invite string) {
	e.Broadcasts.Put(broadcast.Broadcast{
		ID: id, OwnerID: owner, Title: "Show " + id, InviteHash: studio.Hash(invite),
		Stage: stage.Default().JSON(), StageVersion: 1,
	})
}

// Stored returns the stored stage of a broadcast.
func (e *Env) Stored(t *testing.T, id string) stage.State {
	t.Helper()
	b, err := e.Broadcasts.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s, err := stage.Parse(b.Stage)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// RoomStage returns the stage in a room's metadata (zero when the room is not open).
func (e *Env) RoomStage(t *testing.T, id string) stage.State {
	t.Helper()
	r := e.Rooms.Get(stage.Room(id))
	if r == nil {
		return stage.State{}
	}
	s, err := stage.Parse(r.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// JoinGuest joins broadcast id's invite as name with cookie and connects them to the room.
func (e *Env) JoinGuest(t *testing.T, invite, name, cookie string) *studio.GuestJoin {
	t.Helper()
	ctx := context.Background()
	b, err := e.Service.Invite(ctx, invite)
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.Service.JoinAsGuest(ctx, b, name, cookie)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(j.Identity, studio.GuestPrefix) {
		t.Fatalf("identity %q", j.Identity)
	}
	return j
}
