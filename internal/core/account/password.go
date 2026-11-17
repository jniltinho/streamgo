package account

// Portions adapted from MeetHub (MIT): internal/identity/password.go

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the argon2id cost parameters.
type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// defaultArgon2 is the production cost: 64 MiB, 3 passes, 2 lanes.
func defaultArgon2() Argon2Params { return Argon2Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 2} }

var errMalformedHash = errors.New("malformed password hash")

// hasher hashes and verifies passwords, bounding concurrent computations so a sign-in flood
// from many addresses cannot exhaust memory.
type hasher struct {
	params Argon2Params
	slots  chan struct{}
	dummy  string // a hash to verify against for unknown accounts, so timing does not tell them apart
}

func newHasher(p Argon2Params, slots int) *hasher {
	h := &hasher{params: p, slots: make(chan struct{}, slots)}
	h.dummy = h.compute("streamgo-dummy-password", []byte("streamgo-dummy-s"))
	return h
}

func (h *hasher) acquire(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *hasher) release() { <-h.slots }

// hash returns the PHC string of pw ($argon2id$v=19$m=…,t=…,p=…$salt$key).
func (h *hasher) hash(ctx context.Context, pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	return h.compute(pw, salt), nil
}

func (h *hasher) compute(pw string, salt []byte) string {
	p := h.params
	key := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, 32)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, enc.EncodeToString(salt), enc.EncodeToString(key))
}

// verify reports whether pw matches the PHC hash, in constant time.
func (h *hasher) verify(ctx context.Context, encoded, pw string) (bool, error) {
	p, salt, want, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(pw), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(want))) //nolint:gosec // the key is 32 bytes
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func parsePHC(s string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	key, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(key) == 0 {
		return Argon2Params{}, nil, nil, errMalformedHash
	}
	return p, salt, key, nil
}
