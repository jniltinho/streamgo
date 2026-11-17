package livekit_test

import (
	"testing"

	"github.com/livekit/protocol/auth"

	"github.com/jniltinho/streamgo/internal/livekit"
)

// Scenario "Token grants": the decoded token names the room, allows publishing and
// subscribing, and grants neither room creation, administration nor metadata updates.
func TestTokenGrants(t *testing.T) {
	t.Parallel()
	const key, secret = "testkey", "test-secret-that-is-long-enough-0123456789"
	r := livekit.New("http://127.0.0.1:1", key, secret)
	tok, err := r.Token("bc_01J", "host-u1", "Ana Souza")
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth.ParseAPIToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	claims, grants, err := v.Verify(secret)
	if err != nil {
		t.Fatal(err)
	}
	g := grants.Video
	if v.Identity() != "host-u1" || grants.Name != "Ana Souza" || g.Room != "bc_01J" || !g.RoomJoin {
		t.Fatalf("identity %q name %q grant %+v", v.Identity(), grants.Name, g)
	}
	if !g.GetCanPublish() || !g.GetCanSubscribe() {
		t.Fatal("cannot publish or subscribe")
	}
	if g.RoomAdmin || g.RoomCreate || g.RoomList || g.RoomRecord || g.GetCanUpdateOwnMetadata() {
		t.Fatalf("too many grants: %+v", g)
	}
	if ttl := claims.ExpiresAt.Sub(claims.NotBefore.Time); ttl != livekit.TokenTTL {
		t.Fatalf("valid for %v", ttl)
	}
	if _, _, err := v.Verify("another secret"); err == nil {
		t.Fatal("verified with the wrong secret")
	}
}
