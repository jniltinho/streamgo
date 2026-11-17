package output

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

// Publish forwards the owner's WHIP offer to the media server and makes the answer the
// broadcast's one ingest session, closing the previous one (design D3). It returns the answer
// and the new session's opaque id. The media server is called without the broadcast's lock;
// if the run changed meanwhile the new upstream session is closed and the request refused.
func (s *Service) Publish(ctx context.Context, owner, id string, offer []byte) (Answer, string, error) {
	run, err := s.ingestRun(ctx, owner, id)
	if err != nil {
		return Answer{}, "", err
	}
	a, err := s.Ingest.Publish(ctx, id, offer)
	if err != nil {
		return Answer{}, "", err
	}
	ctx = context.WithoutCancel(ctx) // the upstream session exists now: store it or close it
	sid, previous, err := s.storeIngest(ctx, id, run, a.Resource)
	if err != nil {
		s.closeResource(ctx, id, a.Resource)
		return Answer{}, "", err
	}
	if previous != "" {
		s.closeResource(ctx, id, previous)
	}
	return a, sid, nil
}

// Unpublish closes the broadcast's ingest session sid; any other id answers core.ErrNotFound.
func (s *Service) Unpublish(ctx context.Context, owner, id, sid string) error {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return err
	}
	b, err := s.owned(ctx, owner, id)
	if err == nil && (b.Output.IngestID == "" || b.Output.IngestID != sid) {
		err = fmt.Errorf("ingest session: %w", core.ErrNotFound)
	}
	if err != nil {
		unlock()
		return err
	}
	resource := b.Output.IngestURL
	o := b.Output
	o.IngestID, o.IngestURL = "", ""
	err = s.Broadcasts.SetOutput(ctx, id, o)
	unlock()
	if err != nil {
		return fmt.Errorf("store the output of broadcast %s: %w", id, err)
	}
	s.closeResource(context.WithoutCancel(ctx), id, resource)
	return nil
}

// ingestRun checks that owner may publish broadcast id now and returns its run.
func (s *Service) ingestRun(ctx context.Context, owner, id string) (int64, error) {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return 0, err
	}
	defer unlock()
	b, err := s.owned(ctx, owner, id)
	if err != nil {
		return 0, err
	}
	if b.Output.State == broadcast.Offline {
		return 0, conflict("The broadcast is not live; go live before publishing the program.")
	}
	return b.Output.Run, nil
}

// storeIngest makes resource the ingest session of run, returning its new id and the resource
// it replaces.
func (s *Service) storeIngest(ctx context.Context, id string, run int64, resource string) (sid, previous string, err error) {
	unlock, err := s.Locks.Lock(ctx, id)
	if err != nil {
		return "", "", err
	}
	defer unlock()
	b, err := s.Broadcasts.Get(ctx, id)
	if err != nil {
		return "", "", err
	}
	if b.Output.State == broadcast.Offline || b.Output.Run != run {
		return "", "", conflict("The broadcast ended while the program was being published.")
	}
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	o := b.Output
	previous = o.IngestURL
	o.IngestID, o.IngestURL = hex.EncodeToString(raw[:]), resource
	if err := s.Broadcasts.SetOutput(ctx, id, o); err != nil {
		return "", "", fmt.Errorf("store the ingest session of broadcast %s: %w", id, err)
	}
	return o.IngestID, previous, nil
}

func (s *Service) closeResource(ctx context.Context, id, resource string) {
	if resource == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, UpstreamTimeout)
	defer cancel()
	if err := s.Ingest.Close(ctx, resource); err != nil {
		s.logger().Warn("close the ingest session", "broadcast", id, "error", err)
	}
}
