package destination

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/jniltinho/streamgo/internal/core"
)

type fakeResolver map[string]string

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	a, ok := f[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	return []netip.Addr{netip.MustParseAddr(a)}, nil
}

func TestJoin(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ address, key, want string }{
		{"rtmp://a.example.com/live2/", "abcd?x=1", "rtmp://a.example.com/live2/abcd?x=1"},
		{"rtmp://a.example.com/live2", "abcd", "rtmp://a.example.com/live2/abcd"},
		{"rtmp://a.example.com/app/KEY?x=1", "", "rtmp://a.example.com/app/KEY?x=1"},
	} {
		if got := Join(c.address, c.key); got != c.want {
			t.Errorf("Join(%q, %q) = %q, want %q", c.address, c.key, got, c.want)
		}
	}
}

func TestParseAddress(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, address, key, field string
	}{
		{"rtmp", "rtmp://a.rtmp.youtube.com/live2", "k", ""},
		{"rtmps with port", "rtmps://live.example.com:443/app", "", ""},
		{"query without key", "rtmp://h.example.com/app?k=1", "", ""},
		{"http", "http://h.example.com/app", "", "address"},
		{"no host", "rtmp:///app", "", "address"},
		{"space", "rtmp://h.example.com/a b", "", "address"},
		{"line break", "rtmp://h.example.com/a\nb", "", "address"},
		{"too long", "rtmp://h.example.com/" + strings.Repeat("a", MaxAddress), "", "address"},
		{"query with key", "rtmp://h.example.com/app?x=1", "k", "address"},
		{"long key", "rtmp://h.example.com/app", strings.Repeat("k", MaxKey+1), "key"},
		{"key with space", "rtmp://h.example.com/app", "a b", "key"},
	} {
		_, err := parseAddress(c.address, c.key)
		var inv *core.InvalidError
		switch {
		case c.field == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.field != "" && (!errors.As(err, &inv) || inv.Field != c.field):
			t.Errorf("%s: got %v, want invalid %s", c.name, err, c.field)
		}
	}
}

func TestCheckHost(t *testing.T) {
	t.Parallel()
	s := &Service{Resolver: fakeResolver{"public.example.com": "203.0.113.7", "inside.example.com": "10.0.0.5",
		"mapped.example.com": "::ffff:192.168.1.1"}}
	for host, private := range map[string]bool{
		"127.0.0.1": true, "10.0.0.5": true, "::1": true, "169.254.169.254": true, "0.0.0.0": true, "::": true,
		"fe80::1%eth0": true, "fd00::1": true, "inside.example.com": true, "mapped.example.com": true,
		"203.0.113.7": false, "2001:db8::1": false, "public.example.com": false,
	} {
		err := s.checkHost(context.Background(), host)
		if got := errors.Is(err, ErrPrivateAddress); got != private {
			t.Errorf("%s: private %v, err %v", host, got, err)
		}
	}
	if err := s.checkHost(context.Background(), "missing.example.com"); err == nil {
		t.Error("an unresolvable host passed")
	}
	allow := &Service{AllowPrivate: true}
	if err := allow.checkHost(context.Background(), "10.0.0.5"); err != nil {
		t.Errorf("allowed private: %v", err)
	}
}

func TestRedact(t *testing.T) {
	t.Parallel()
	address, key := "rtmp://user:pw@a.example.com/live2/", "abcd?x=1"
	tg := Target{URL: Join(address, key), secrets: secretParts(address, key, Join(address, key))}
	text := tg.Redact("Error opening rtmp://user:pw@a.example.com/live2/abcd?x=1: live2/abcd?x=1 abcd x=1 rejected")
	for _, secret := range []string{"live2", "abcd", "x=1", "pw", "user"} {
		if strings.Contains(text, secret) {
			t.Errorf("%q survives in %q", secret, text)
		}
	}
	if !strings.Contains(text, "a.example.com") {
		t.Errorf("the host was redacted: %q", text)
	}
}
