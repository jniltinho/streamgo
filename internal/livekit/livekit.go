// Package livekit is the studio's Rooms port (core/studio) over LiveKit's server SDK v2:
// access tokens, rooms with their metadata, and participants.
//
// Portions adapted from MeetHub (MIT): internal/meetings/livekit.go
package livekit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/twitchtv/twirp"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/studio"
)

// TokenTTL is how long a studio token is valid.
const TokenTTL = 10 * time.Minute

// EmptyTimeout closes a room that stayed empty this long (design D1).
const EmptyTimeout = 300

// Rooms implements studio.Rooms.
type Rooms struct {
	client      *lksdk.RoomServiceClient
	key, secret string
}

var _ studio.Rooms = (*Rooms)(nil)

// New reaches LiveKit's room API at apiURL (http or https) with the API key and secret.
func New(apiURL, key, secret string) *Rooms {
	return &Rooms{client: lksdk.NewRoomServiceClient(apiURL, key, secret), key: key, secret: secret}
}

func notFound(err error) bool {
	var te twirp.Error
	return errors.As(err, &te) && te.Code() == twirp.NotFound
}

// Token signs a token to join room as identity: publish and subscribe, no room creation,
// administration or metadata updates.
func (r *Rooms) Token(room, identity, name string) (string, error) {
	grant := &auth.VideoGrant{RoomJoin: true, Room: room}
	grant.SetCanPublish(true)
	grant.SetCanSubscribe(true)
	grant.SetCanUpdateOwnMetadata(false)
	tok, err := auth.NewAccessToken(r.key, r.secret).
		SetVideoGrant(grant).SetIdentity(identity).SetName(name).SetValidFor(TokenTTL).ToJWT()
	if err != nil {
		return "", fmt.Errorf("livekit token: %w", err)
	}
	return tok, nil
}

// CreateRoom opens room with the metadata; LiveKit returns an existing room unchanged.
func (r *Rooms) CreateRoom(ctx context.Context, room, metadata string, maxParticipants int) error {
	_, err := r.client.CreateRoom(ctx, &livekit.CreateRoomRequest{
		Name: room, Metadata: metadata, EmptyTimeout: EmptyTimeout, MaxParticipants: uint32(max(maxParticipants, 0)), //nolint:gosec // bounded by the configuration
	})
	if err != nil {
		return fmt.Errorf("livekit create room: %w", err)
	}
	return nil
}

// UpdateMetadata replaces room's metadata; a missing room wraps core.ErrNotFound.
func (r *Rooms) UpdateMetadata(ctx context.Context, room, metadata string) error {
	_, err := r.client.UpdateRoomMetadata(ctx, &livekit.UpdateRoomMetadataRequest{Room: room, Metadata: metadata})
	if notFound(err) {
		return fmt.Errorf("livekit room %s: %w", room, core.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("livekit update metadata: %w", err)
	}
	return nil
}

// ListRooms lists the open rooms with their metadata.
func (r *Rooms) ListRooms(ctx context.Context) ([]studio.Room, error) {
	res, err := r.client.ListRooms(ctx, &livekit.ListRoomsRequest{})
	if err != nil {
		return nil, fmt.Errorf("livekit list rooms: %w", err)
	}
	out := make([]studio.Room, 0, len(res.GetRooms()))
	for _, rm := range res.GetRooms() {
		out = append(out, studio.Room{Name: rm.GetName(), Metadata: rm.GetMetadata()})
	}
	return out, nil
}

// ListParticipants lists who is in room; none when the room does not exist.
func (r *Rooms) ListParticipants(ctx context.Context, room string) ([]studio.Participant, error) {
	res, err := r.client.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: room})
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("livekit list participants: %w", err)
	}
	out := make([]studio.Participant, 0, len(res.GetParticipants()))
	for _, p := range res.GetParticipants() {
		joined := p.GetJoinedAtMs()
		if joined == 0 {
			joined = p.GetJoinedAt() * 1000
		}
		out = append(out, studio.Participant{Identity: p.GetIdentity(), JoinedAt: time.UnixMilli(joined).UTC()})
	}
	return out, nil
}

// RemoveParticipant disconnects identity; someone already gone is not an error.
func (r *Rooms) RemoveParticipant(ctx context.Context, room, identity string) error {
	_, err := r.client.RemoveParticipant(ctx, &livekit.RoomParticipantIdentity{Room: room, Identity: identity})
	if err != nil && !notFound(err) {
		return fmt.Errorf("livekit remove participant: %w", err)
	}
	return nil
}

// DeleteRoom closes room and disconnects everyone in it; a missing room is not an error.
func (r *Rooms) DeleteRoom(ctx context.Context, room string) error {
	_, err := r.client.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: room})
	if err != nil && !notFound(err) {
		return fmt.Errorf("livekit delete room: %w", err)
	}
	return nil
}

// Ready is the readiness check: LiveKit's room API answers with this key.
func (r *Rooms) Ready(ctx context.Context) error {
	_, err := r.client.ListRooms(ctx, &livekit.ListRoomsRequest{Names: []string{"streamgo-readiness"}})
	return err
}
