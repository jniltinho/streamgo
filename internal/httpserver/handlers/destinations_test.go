package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
)

type destView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	HasKey bool   `json:"has_key"`
}

func (e *env) createDestination(user, name, address, key string) destView {
	e.t.Helper()
	body := map[string]string{"name": name, "address": address}
	if key != "" {
		body["key"] = key
	}
	var d destView
	e.call(user, http.MethodPost, "/destinations", body, http.StatusCreated, &d)
	return d
}

func (e *env) target(id string) string {
	e.t.Helper()
	tg, err := e.ds.Target(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return tg.URL
}

func (e *env) selectDestinations(user, broadcastID string, ids ...string) (int, []byte) {
	e.t.Helper()
	return e.do(user, http.MethodPut, "/broadcasts/"+broadcastID+"/destinations", map[string][]string{"ids": ids})
}

func (e *env) selected(broadcastID string) []string {
	e.t.Helper()
	var d detail
	e.call(ana, http.MethodGet, "/broadcasts/"+broadcastID, nil, http.StatusOK, &d)
	var ids []string
	for _, x := range d.Destinations {
		ids = append(ids, x.ID)
	}
	slices.Sort(ids)
	return ids
}

// Scenario "Create".
func TestCreateDestination(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	d := e.createDestination(ana, "YouTube", "rtmp://a.rtmp.youtube.com/live2", "xxxx-yyyy")
	if d.Name != "YouTube" || d.Scheme != "rtmp" || d.Host != "a.rtmp.youtube.com" || !d.HasKey {
		t.Fatalf("created: %+v", d)
	}
	var list struct{ Destinations []destView }
	e.call(ana, http.MethodGet, "/destinations", nil, http.StatusOK, &list)
	if len(list.Destinations) != 1 || list.Destinations[0] != d {
		t.Fatalf("ana's list: %+v", list)
	}
	e.call(bruno, http.MethodGet, "/destinations", nil, http.StatusOK, &list)
	if len(list.Destinations) != 0 {
		t.Fatalf("bruno's list: %+v", list)
	}
	for _, m := range []string{http.MethodPatch, http.MethodDelete} {
		if code, _ := e.do(bruno, m, "/destinations/"+d.ID, map[string]string{"name": "x"}); code != http.StatusNotFound {
			t.Errorf("%s by bruno: %d", m, code)
		}
	}
}

// Scenarios "Invalid address" and "Query in the address with a key".
func TestInvalidDestination(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, c := range []struct{ name, address, key, field string }{
		{"Web", "http://live.example.com/app", "", "address"},
		{"No host", "rtmp:///app", "", "address"},
		{"Space", "rtmp://live.example.com/a b", "", "address"},
		{"Line break", "rtmp://live.example.com/a\nb", "", "address"},
		{"  ", "rtmp://live.example.com/app", "", "name"},
		{strings.Repeat("n", 61), "rtmp://live.example.com/app", "", "name"},
		{"Query and key", "rtmp://live.example.com/app?x=1", "k", "address"},
		{"Long key", "rtmp://live.example.com/app", strings.Repeat("k", 513), "key"},
	} {
		code, raw := e.do(ana, http.MethodPost, "/destinations", map[string]string{"name": c.name, "address": c.address, "key": c.key})
		assertInvalid(t, code, raw, c.field)
	}
	d := e.createDestination(ana, "Mine", "rtmp://live.example.com/app?x=1", "")
	code, raw := e.do(ana, http.MethodPatch, "/destinations/"+d.ID, map[string]string{"key": "k"})
	assertInvalid(t, code, raw, "address")
	if !strings.Contains(string(raw), "query") {
		t.Fatalf("message: %s", raw)
	}
}

// Scenario "Address and key are joined".
func TestAddressAndKeyAreJoined(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	d := e.createDestination(ana, "Example", "rtmp://a.example.com/live2/", "abcd?x=1")
	if got := e.target(d.ID); got != "rtmp://a.example.com/live2/abcd?x=1" {
		t.Fatalf("target %q", got)
	}
	plain := e.createDestination(ana, "Plain", "rtmp://a.example.com/app/KEY", "")
	if got := e.target(plain.ID); got != "rtmp://a.example.com/app/KEY" {
		t.Fatalf("target without key %q", got)
	}
}

// Scenario "Limit".
func TestDestinationLimit(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for i := range destination.MaxPerUser {
		e.createDestination(ana, fmt.Sprint("D", i), "rtmp://live.example.com/app", "")
	}
	code, raw := e.do(ana, http.MethodPost, "/destinations", map[string]string{"name": "One more", "address": "rtmp://live.example.com/app"})
	if code != http.StatusUnprocessableEntity || !strings.Contains(string(raw), "limit") {
		t.Fatalf("21st: %d %s", code, raw)
	}
	e.createDestination(bruno, "Bruno's", "rtmp://live.example.com/app", "")
}

// Scenarios "Nothing secret returned" and "Key pasted in the address".
func TestNothingSecretReturned(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	raw := e.call(ana, http.MethodPost, "/destinations",
		map[string]string{"name": "Pasted", "address": "rtmps://live.example.com:443/app/SECRET123?token=QUERY456"},
		http.StatusCreated, nil)
	var d destView
	_ = json.Unmarshal(raw, &d)
	if d.Host != "live.example.com:443" || d.Scheme != "rtmps" || d.HasKey {
		t.Fatalf("created: %+v", d)
	}
	b := e.createBroadcast(ana, "Show")
	e.call(ana, http.MethodPut, "/broadcasts/"+b.ID+"/destinations", map[string][]string{"ids": {d.ID}}, http.StatusOK, nil)
	responses := [][]byte{raw,
		e.call(ana, http.MethodGet, "/destinations", nil, http.StatusOK, nil),
		e.call(ana, http.MethodPatch, "/destinations/"+d.ID, map[string]string{"name": "Renamed"}, http.StatusOK, nil),
		e.call(ana, http.MethodGet, "/broadcasts/"+b.ID, nil, http.StatusOK, nil),
	}
	for _, r := range responses {
		for _, secret := range []string{"SECRET123", "QUERY456", "/app"} {
			if strings.Contains(string(r), secret) {
				t.Fatalf("%q in %s", secret, r)
			}
		}
	}
	var list struct{ Destinations []map[string]any }
	_ = json.Unmarshal(responses[1], &list)
	keys := slices.Sorted(maps.Keys(list.Destinations[0]))
	if !slices.Equal(keys, []string{"has_key", "host", "id", "name", "scheme"}) {
		t.Fatalf("destination fields %v", keys)
	}
	assertNotInDatabase(t, e.db, "SECRET123")
	assertNotInDatabase(t, e.db, "QUERY456")
}

// Scenario "Rename keeps the address and the key"; an empty key clears it.
func TestRenameKeepsAddressAndKey(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	d := e.createDestination(ana, "YouTube", "rtmp://a.rtmp.youtube.com/live2", "xxxx-yyyy")
	var got destView
	e.call(ana, http.MethodPatch, "/destinations/"+d.ID, map[string]string{"name": "YT"}, http.StatusOK, &got)
	if got.Name != "YT" || !got.HasKey || e.target(d.ID) != "rtmp://a.rtmp.youtube.com/live2/xxxx-yyyy" {
		t.Fatalf("renamed: %+v %s", got, e.target(d.ID))
	}
	e.call(ana, http.MethodPatch, "/destinations/"+d.ID, map[string]string{"address": "rtmp://b.example.com/app"}, http.StatusOK, &got)
	if got.Host != "b.example.com" || e.target(d.ID) != "rtmp://b.example.com/app/xxxx-yyyy" {
		t.Fatalf("new address: %+v %s", got, e.target(d.ID))
	}
	e.call(ana, http.MethodPatch, "/destinations/"+d.ID, map[string]string{"key": ""}, http.StatusOK, &got)
	if got.HasKey || e.target(d.ID) != "rtmp://b.example.com/app" {
		t.Fatalf("key cleared: %+v %s", got, e.target(d.ID))
	}
}

// Scenarios "Loopback and private hosts" and "Operator opt-in".
func TestPrivateAddresses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	hosts := []string{"127.0.0.1", "localhost", "10.0.0.5", "[::1]", "169.254.169.254"}
	for _, h := range hosts {
		code, raw := e.do(ana, http.MethodPost, "/destinations", map[string]string{"name": "P", "address": "rtmp://" + h + "/app"})
		assertInvalid(t, code, raw, "address")
		if !strings.Contains(string(raw), "Private addresses are not allowed") {
			t.Fatalf("%s: %s", h, raw)
		}
	}
	d := e.createDestination(ana, "Public", "rtmp://live.example.com/app", "")
	code, raw := e.do(ana, http.MethodPatch, "/destinations/"+d.ID, map[string]string{"address": "rtmp://10.0.0.5/app"})
	assertInvalid(t, code, raw, "address")

	open := newEnv(t, func(s *destination.Service) { s.AllowPrivate = true })
	for _, h := range hosts {
		open.createDestination(ana, "Mine", "rtmp://"+h+"/app", "")
	}
}

// Scenarios "Choose destinations", "Someone else's destination" and "Too many".
func TestChooseDestinations(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(s *destination.Service) {
		s.Statuses = statuses{} // a provider that knows nothing: every status is off
	})
	b := e.createBroadcast(ana, "Show")
	d1 := e.createDestination(ana, "One", "rtmp://one.example.com/app", "")
	d2 := e.createDestination(ana, "Two", "rtmp://two.example.com/app", "")
	d3 := e.createDestination(ana, "Three", "rtmp://three.example.com/app", "")
	foreign := e.createDestination(bruno, "Bruno's", "rtmp://b.example.com/app", "")

	var out struct {
		Destinations []struct {
			ID, Name, Status, Error string
			HasKey                  bool `json:"has_key"`
		}
	}
	e.call(ana, http.MethodPut, "/broadcasts/"+b.ID+"/destinations", map[string][]string{"ids": {d2.ID, d1.ID, d1.ID}}, http.StatusOK, &out)
	if len(out.Destinations) != 2 || out.Destinations[0].Status != destination.Off {
		t.Fatalf("chosen: %+v", out)
	}
	want := []string{d1.ID, d2.ID}
	slices.Sort(want)
	if got := e.selected(b.ID); !slices.Equal(got, want) {
		t.Fatalf("listed %v, want %v", got, want)
	}

	code, raw := e.selectDestinations(ana, b.ID, d1.ID, foreign.ID)
	assertInvalid(t, code, raw, "ids")
	code, raw = e.selectDestinations(ana, b.ID, "01NOSUCHDESTINATION0000000")
	assertInvalid(t, code, raw, "ids")
	code, raw = e.selectDestinations(ana, b.ID, d1.ID, d2.ID, d3.ID)
	assertInvalid(t, code, raw, "ids")
	if got := e.selected(b.ID); !slices.Equal(got, want) {
		t.Fatalf("selection changed to %v", got)
	}
	if code, _ := e.selectDestinations(bruno, b.ID, foreign.ID); code != http.StatusNotFound {
		t.Fatalf("bruno on ana's broadcast: %d", code)
	}
	e.call(ana, http.MethodPut, "/broadcasts/"+b.ID+"/destinations", map[string][]string{"ids": {}}, http.StatusOK, nil)
	if got := e.selected(b.ID); len(got) != 0 {
		t.Fatalf("cleared: %v", got)
	}
}

// The statuses come from the live-output package's provider; while the broadcast is not
// offline, a change goes through its notifier, which may refuse it.
func TestSelectionWhileLive(t *testing.T) {
	t.Parallel()
	n := &notifier{}
	var st statuses
	e := newEnv(t, func(s *destination.Service) { s.Notifier = n; s.Statuses = statuses{} })
	b := e.createBroadcast(ana, "Show")
	d1 := e.createDestination(ana, "One", "rtmp://one.example.com/app", "")
	d2 := e.createDestination(ana, "Two", "rtmp://two.example.com/app", "")
	e.call(ana, http.MethodPut, "/broadcasts/"+b.ID+"/destinations", map[string][]string{"ids": {d1.ID}}, http.StatusOK, nil)
	if n.added != nil || n.removed != nil {
		t.Fatalf("notified while offline: %+v", n)
	}
	e.setState(b.ID, broadcast.Live)
	st = statuses{d1.ID: {State: destination.Retrying, Error: "connection refused by one.example.com"}}
	e.ds.Statuses = st

	var d detail
	e.call(ana, http.MethodGet, "/broadcasts/"+b.ID, nil, http.StatusOK, &d)
	if len(d.Destinations) != 1 || d.Destinations[0].Status != destination.Retrying || d.Destinations[0].Error != st[d1.ID].Error {
		t.Fatalf("statuses: %+v", d.Destinations)
	}

	e.call(ana, http.MethodPut, "/broadcasts/"+b.ID+"/destinations", map[string][]string{"ids": {d2.ID}}, http.StatusOK, nil)
	if !slices.Equal(n.added, []string{d2.ID}) || !slices.Equal(n.removed, []string{d1.ID}) {
		t.Fatalf("notified %+v", n)
	}
	n.err = fmt.Errorf("the last destination: %w", core.ErrConflict)
	if code, _ := e.selectDestinations(ana, b.ID); code != http.StatusConflict {
		t.Fatalf("refused change: %d", code)
	}
	if got := e.selected(b.ID); !slices.Equal(got, []string{d2.ID}) {
		t.Fatalf("selection after a refusal: %v", got)
	}
}

// Scenarios "Delete while in use", "In use while interrupted" and "Delete when free".
func TestDestinationsInUse(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	busy := e.createBroadcast(ana, "Busy")
	idle := e.createBroadcast(ana, "Idle")
	used := e.createDestination(ana, "Used", "rtmp://used.example.com/app", "key1")
	free := e.createDestination(ana, "Free", "rtmp://free.example.com/app", "key2")
	for _, b := range []string{busy.ID, idle.ID} {
		e.call(ana, http.MethodPut, "/broadcasts/"+b+"/destinations", map[string][]string{"ids": {used.ID, free.ID}}, http.StatusOK, nil)
	}
	e.call(ana, http.MethodPut, "/broadcasts/"+busy.ID+"/destinations", map[string][]string{"ids": {used.ID}}, http.StatusOK, nil)

	for _, state := range []broadcast.OutputState{broadcast.Live, broadcast.Interrupted, broadcast.Starting} {
		e.setState(busy.ID, state)
		for _, req := range []struct {
			method string
			body   any
		}{
			{http.MethodDelete, nil},
			{http.MethodPatch, map[string]string{"key": "other"}},
			{http.MethodPatch, map[string]string{"address": "rtmp://elsewhere.example.com/app"}},
		} {
			code, raw := e.do(ana, req.method, "/destinations/"+used.ID, req.body)
			if code != http.StatusConflict || !strings.Contains(string(raw), "used by a broadcast") {
				t.Fatalf("%s %s %v: %d %s", state, req.method, req.body, code, raw)
			}
		}
		// renaming is allowed, and so is sending the same key again
		e.call(ana, http.MethodPatch, "/destinations/"+used.ID, map[string]string{"name": "Used " + string(state), "key": "key1"}, http.StatusOK, nil)
		if got := e.target(used.ID); got != "rtmp://used.example.com/app/key1" {
			t.Fatalf("target changed: %s", got)
		}
	}

	e.call(ana, http.MethodDelete, "/destinations/"+free.ID, nil, http.StatusNoContent, nil)
	if got := e.selected(idle.ID); !slices.Equal(got, []string{used.ID}) {
		t.Fatalf("idle broadcast lists %v", got)
	}
	if _, err := e.ds.Target(context.Background(), free.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("deleted destination: %v", err)
	}
	e.setState(busy.ID, broadcast.Offline)
	e.call(ana, http.MethodDelete, "/destinations/"+used.ID, nil, http.StatusNoContent, nil)
	if got := e.selected(busy.ID); len(got) != 0 {
		t.Fatalf("busy broadcast lists %v", got)
	}
}
