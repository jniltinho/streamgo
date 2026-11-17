// Package destination is the destination domain (work package B): a user's RTMP destinations
// with their sealed address and key, the private-address check, the destinations each
// broadcast uses and the rule that keeps a destination in use unchanged (design D5, D8).
package destination

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

// Limits of the destinations requirement.
const (
	MaxPerUser = 20
	MaxName    = 60
	MaxAddress = 1024
	MaxKey     = 512
)

// ErrPrivateAddress is a destination whose host is, or resolves to, a loopback, private,
// link-local or unspecified address while output.allow_private_destinations is off.
var ErrPrivateAddress = errors.New("the destination's host is a private address, which is not allowed")

// Destination is one row of the destinations table. Sealed holds the address and the key.
type Destination struct {
	ID      string
	OwnerID string
	Name    string
	Scheme  string
	Host    string
	HasKey  bool
	Sealed  []byte
}

// Repository stores destinations and the selections of broadcasts; internal/store implements
// it. Get returns an error wrapping core.ErrNotFound for a missing destination; Create sets ID.
type Repository interface {
	Create(ctx context.Context, d *Destination) error
	Get(ctx context.Context, id string) (*Destination, error)
	ListByOwner(ctx context.Context, ownerID string) ([]Destination, error)
	Update(ctx context.Context, d *Destination) error
	// Delete removes the destination and every broadcast's selection of it.
	Delete(ctx context.Context, id string) error
	// InUse reports whether a broadcast that is not offline chose the destination.
	InUse(ctx context.Context, id string) (bool, error)
	ListByBroadcast(ctx context.Context, broadcastID string) ([]Destination, error)
	SetSelection(ctx context.Context, broadcastID string, ids []string) error
}

// Relay states shown per destination (design D5).
const (
	Off        = "off"
	Connecting = "connecting"
	Live       = "live"
	Retrying   = "retrying"
)

// Status is a destination's relay status in a broadcast; Error is already redacted.
type Status struct {
	State string
	Error string
}

// Statuses reports the relay status of a broadcast's destinations, by destination id; the
// live-output package implements it. A destination missing from the map is Off.
type Statuses interface {
	StatusOf(broadcastID string) map[string]Status
}

// SelectionNotifier is told when the selection of a broadcast that is not offline changes,
// before it is stored, while the broadcast's lock is held. The live-output package implements
// it: it starts and stops the relays and refuses (core.ErrConflict) removing the last
// destination or adding one that another active broadcast uses.
type SelectionNotifier interface {
	SelectionChanged(ctx context.Context, broadcastID string, added, removed []string) error
}

// Resolver resolves host names; *net.Resolver implements it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Service manages destinations for their owners: a destination or broadcast the owner does
// not have answers core.ErrNotFound.
type Service struct {
	Repo            Repository
	Broadcasts      broadcast.Repository
	Locks           *broadcast.Locks
	Sealer          broadcast.Sealer
	Resolver        Resolver          // nil: net.DefaultResolver
	AllowPrivate    bool              // output.allow_private_destinations
	MaxPerBroadcast int               // output.max_destinations
	Statuses        Statuses          // nil: every status is Off
	Notifier        SelectionNotifier // nil: selections are only stored
}

// Input is a new destination.
type Input struct{ Name, Address, Key string }

// Patch changes a destination: a nil field keeps the stored value, an empty Key clears it.
type Patch struct{ Name, Address, Key *string }

// Selected is a destination chosen for a broadcast with its relay status.
type Selected struct {
	Destination
	Status Status
}

// List lists the owner's destinations.
func (s *Service) List(ctx context.Context, owner string) ([]Destination, error) {
	return s.Repo.ListByOwner(ctx, owner)
}

// Create saves a new destination for owner.
func (s *Service) Create(ctx context.Context, owner string, in Input) (*Destination, error) {
	name, err := validName(in.Name)
	if err != nil {
		return nil, err
	}
	d := &Destination{OwnerID: owner, Name: name}
	if err := s.setAddress(ctx, d, in.Address, in.Key); err != nil {
		return nil, err
	}
	// shortcut: count then insert is not atomic, so concurrent creates may pass 20 by a few; a per-owner lock if it matters
	list, err := s.Repo.ListByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	if len(list) >= MaxPerUser {
		return nil, core.Invalid("", fmt.Sprintf("You can keep at most %d destinations; the limit was reached.", MaxPerUser))
	}
	if err := s.Repo.Create(ctx, d); err != nil {
		return nil, fmt.Errorf("create destination: %w", err)
	}
	return d, nil
}

// Update changes the owner's destination. Changing the address or the key of a destination
// that an active broadcast uses answers core.ErrConflict; renaming it is allowed.
func (s *Service) Update(ctx context.Context, owner, id string, p Patch) (*Destination, error) {
	d, err := s.get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		if d.Name, err = validName(*p.Name); err != nil {
			return nil, err
		}
	}
	if p.Address != nil || p.Key != nil {
		address, key, err := s.open(d)
		if err != nil {
			return nil, err
		}
		newAddress, newKey := address, key
		if p.Address != nil {
			newAddress = *p.Address
		}
		if p.Key != nil {
			newKey = *p.Key
		}
		if newAddress != address || newKey != key {
			// shortcut: checked before writing, not under the broadcasts' locks, so a go live in between is not seen; the output package claims destinations under its own lock
			if err := s.checkFree(ctx, id); err != nil {
				return nil, err
			}
			if err := s.setAddress(ctx, d, newAddress, newKey); err != nil {
				return nil, err
			}
		}
	}
	if err := s.Repo.Update(ctx, d); err != nil {
		return nil, fmt.Errorf("update destination: %w", err)
	}
	return d, nil
}

// Delete deletes the owner's destination and removes it from every broadcast; it answers
// core.ErrConflict while an active broadcast uses it.
func (s *Service) Delete(ctx context.Context, owner, id string) error {
	if _, err := s.get(ctx, owner, id); err != nil {
		return err
	}
	if err := s.checkFree(ctx, id); err != nil {
		return err
	}
	if err := s.Repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete destination: %w", err)
	}
	return nil
}

// ForBroadcast lists the destinations chosen for a broadcast with their status. The caller
// has checked that the user owns the broadcast.
func (s *Service) ForBroadcast(ctx context.Context, broadcastID string) ([]Selected, error) {
	list, err := s.Repo.ListByBroadcast(ctx, broadcastID)
	if err != nil {
		return nil, err
	}
	var statuses map[string]Status
	if s.Statuses != nil {
		statuses = s.Statuses.StatusOf(broadcastID)
	}
	out := make([]Selected, len(list))
	for i, d := range list {
		st, ok := statuses[d.ID]
		if !ok {
			st = Status{State: Off}
		}
		out[i] = Selected{Destination: d, Status: st}
	}
	return out, nil
}

// Select sets the destinations the owner's broadcast uses: at most MaxPerBroadcast, all the
// owner's own. While the broadcast is not offline the change goes through the Notifier first.
func (s *Service) Select(ctx context.Context, owner, broadcastID string, ids []string) ([]Selected, error) {
	unlock, err := s.Locks.Lock(ctx, broadcastID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	b, err := s.Broadcasts.Get(ctx, broadcastID)
	if err != nil {
		return nil, err
	}
	if b.OwnerID != owner {
		return nil, fmt.Errorf("broadcast %s: %w", broadcastID, core.ErrNotFound)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > s.MaxPerBroadcast {
		return nil, core.Invalid("ids", fmt.Sprintf("A broadcast can use at most %d destinations.", s.MaxPerBroadcast))
	}
	for _, id := range ids {
		if _, err := s.get(ctx, owner, id); errors.Is(err, core.ErrNotFound) {
			return nil, core.Invalid("ids", "One of the destinations does not exist.")
		} else if err != nil {
			return nil, err
		}
	}
	if b.Output.State != broadcast.Offline && s.Notifier != nil {
		current, err := s.Repo.ListByBroadcast(ctx, broadcastID)
		if err != nil {
			return nil, err
		}
		var added, removed []string
		for _, d := range current {
			if !slices.Contains(ids, d.ID) {
				removed = append(removed, d.ID)
			}
		}
		for _, id := range ids {
			if !slices.ContainsFunc(current, func(d Destination) bool { return d.ID == id }) {
				added = append(added, id)
			}
		}
		if len(added)+len(removed) > 0 {
			if err := s.Notifier.SelectionChanged(ctx, broadcastID, added, removed); err != nil {
				return nil, err
			}
		}
	}
	if err := s.Repo.SetSelection(ctx, broadcastID, ids); err != nil {
		return nil, fmt.Errorf("set the destinations of broadcast %s: %w", broadcastID, err)
	}
	return s.ForBroadcast(ctx, broadcastID)
}

// Target is where a relay publishes, and the redaction of everything secret in it.
type Target struct {
	URL     string
	secrets []string // longest first
}

// Target returns where a relay of the destination publishes, checking again that its host does
// not resolve to a private address (an error wrapping ErrPrivateAddress, safe to show). The
// live-output package calls it before each relay starts and redacts the relay's output with
// the result.
func (s *Service) Target(ctx context.Context, id string) (Target, error) {
	d, err := s.Repo.Get(ctx, id)
	if err != nil {
		return Target{}, err
	}
	address, key, err := s.open(d)
	if err != nil {
		return Target{}, err
	}
	u, err := url.Parse(address)
	if err != nil {
		return Target{}, fmt.Errorf("destination %s has an invalid stored address", id)
	}
	if err := s.checkHost(ctx, u.Hostname()); err != nil {
		return Target{}, err
	}
	t := Target{URL: Join(address, key)}
	t.secrets = secretParts(address, key, t.URL)
	return t, nil
}

// Redact replaces in text every part of the target after its host, and the key, with "***".
func (t Target) Redact(text string) string {
	for _, p := range t.secrets {
		text = strings.ReplaceAll(text, p, "***")
	}
	return text
}

// Join is the URL a relay publishes to: the address alone without a key, otherwise the address
// without its trailing slash, a slash and the key (which may carry its own query).
func Join(address, key string) string {
	if key == "" {
		return address
	}
	return strings.TrimSuffix(address, "/") + "/" + key
}

func (s *Service) get(ctx context.Context, owner, id string) (*Destination, error) {
	d, err := s.Repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if d.OwnerID != owner {
		return nil, fmt.Errorf("destination %s: %w", id, core.ErrNotFound)
	}
	return d, nil
}

func (s *Service) checkFree(ctx context.Context, id string) error {
	used, err := s.Repo.InUse(ctx, id)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("destination %s is used by an active broadcast: %w", id, core.ErrConflict)
	}
	return nil
}

// setAddress validates address and key and stores them sealed on d, with the scheme and host.
func (s *Service) setAddress(ctx context.Context, d *Destination, address, key string) error {
	u, err := parseAddress(address, key)
	if err != nil {
		return err
	}
	if err := s.checkHost(ctx, u.Hostname()); errors.Is(err, ErrPrivateAddress) {
		return core.Invalid("address", "Private addresses are not allowed.")
	} else if err != nil {
		return core.Invalid("address", "The host of the address cannot be resolved.")
	}
	sealed, err := s.Sealer.Seal(bound(d.OwnerID), []byte(address+"\n"+key))
	if err != nil {
		return fmt.Errorf("seal the destination: %w", err)
	}
	d.Scheme, d.Host, d.HasKey, d.Sealed = u.Scheme, u.Host, key != "", sealed
	return nil
}

func (s *Service) open(d *Destination) (address, key string, err error) {
	plain, err := s.Sealer.Open(bound(d.OwnerID), d.Sealed)
	if err != nil {
		return "", "", fmt.Errorf("open destination %s: %w", d.ID, err)
	}
	address, key, _ = strings.Cut(string(plain), "\n")
	return address, key, nil
}

// checkHost refuses a host name (url.URL.Hostname) that is, or resolves to, a non-public address (unless allowed).
func (s *Service) checkHost(ctx context.Context, host string) error {
	if s.AllowPrivate {
		return nil
	}
	host, _, _ = strings.Cut(host, "%") // an IPv6 zone
	addrs := []netip.Addr{}
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = append(addrs, a)
	} else {
		r := s.Resolver
		if r == nil {
			r = net.DefaultResolver
		}
		if addrs, err = r.LookupNetIP(ctx, "ip", host); err != nil {
			return fmt.Errorf("resolve %s: %w", host, err)
		}
	}
	for _, a := range addrs {
		a = a.Unmap()
		if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsUnspecified() {
			return fmt.Errorf("%s: %w", host, ErrPrivateAddress)
		}
	}
	return nil
}

// parseAddress validates an address and key; errors never quote them.
func parseAddress(address, key string) (*url.URL, error) {
	if len(address) > MaxAddress {
		return nil, core.Invalid("address", fmt.Sprintf("must be at most %d characters", MaxAddress))
	}
	if hasSpaceOrControl(address) {
		return nil, core.Invalid("address", "must not contain spaces or control characters")
	}
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "rtmp" && u.Scheme != "rtmps") || u.Hostname() == "" {
		return nil, core.Invalid("address", "must be an rtmp:// or rtmps:// address with a host")
	}
	if len(key) > MaxKey {
		return nil, core.Invalid("key", fmt.Sprintf("must be at most %d characters", MaxKey))
	}
	if hasSpaceOrControl(key) {
		return nil, core.Invalid("key", "must not contain spaces or control characters")
	}
	if key != "" && (u.RawQuery != "" || u.ForceQuery) {
		return nil, core.Invalid("address", "Give the query string with the key, not in the address.")
	}
	return u, nil
}

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > MaxName {
		return "", core.Invalid("name", fmt.Sprintf("must be 1 to %d characters", MaxName))
	}
	return name, nil
}

func hasSpaceOrControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// secretParts lists what must never appear in output: the key and everything of the address
// and the URL after the host (the path, its segments, the query, user information).
func secretParts(address, key, target string) []string {
	parts := []string{key}
	for _, raw := range []string{address, target} {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		_, rest, _ := strings.Cut(strings.TrimPrefix(raw, u.Scheme+"://"), "/")
		parts = append(parts, rest, u.Path, u.RawQuery, u.User.String())
		for seg := range strings.SplitSeq(u.Path, "/") {
			parts = append(parts, seg)
		}
	}
	slices.SortFunc(parts, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	parts = slices.Compact(parts)
	return slices.DeleteFunc(parts, func(p string) bool { return p == "" })
}

func bound(owner string) string { return "destination:" + owner }
