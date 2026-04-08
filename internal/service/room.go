package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"calixio/internal/livekit"
	"calixio/internal/repository"
)

var ErrRoomEnded = errors.New("room is ended")
var ErrRoomForbidden = errors.New("room access forbidden")
var ErrMediaRequiredForMovieMode = errors.New("media is required for movie mode")
var ErrMediaForbiddenForRoom = errors.New("media is forbidden for room")

type RoomService struct {
	rooms repository.RoomRepository
	media repository.MediaRepository
	lk    *livekit.Client
	clock func() time.Time
}

func NewRoomService(rooms repository.RoomRepository, media repository.MediaRepository, lk *livekit.Client) *RoomService {
	return &RoomService{rooms: rooms, media: media, lk: lk, clock: time.Now}
}

type CreateRoomInput struct {
	Name        string
	OwnerUserID string
	MediaID     *string
}

func (s *RoomService) CreateRoom(ctx context.Context, in CreateRoomInput) (repository.Room, error) {
	roomID, err := newID()
	if err != nil {
		return repository.Room{}, err
	}
	name := in.Name
	if name == "" {
		name = "room-" + roomID
	}

	if err := s.lk.CreateRoom(ctx, name); err != nil {
		return repository.Room{}, err
	}

	room := repository.Room{
		ID:          roomID,
		Name:        name,
		OwnerUserID: in.OwnerUserID,
		MediaID:     in.MediaID,
		Status:      repository.RoomActive,
		CreatedAt:   s.clock(),
	}
	return s.rooms.CreateRoom(ctx, room)
}

func (s *RoomService) ListRoomsByOwner(ctx context.Context, ownerUserID string) ([]repository.Room, error) {
	return s.rooms.ListRoomsByOwner(ctx, ownerUserID)
}

func (s *RoomService) JoinRoom(ctx context.Context, roomID, identity, participantName string) (string, repository.Room, error) {
	room, err := s.rooms.GetRoomByID(ctx, roomID)
	if err != nil {
		return "", repository.Room{}, err
	}
	if room.Status != repository.RoomActive {
		return "", repository.Room{}, ErrRoomEnded
	}
	jwt, err := s.lk.GenerateToken(identity, room.Name, participantName)
	if err != nil {
		return "", repository.Room{}, err
	}
	return jwt, room, nil
}

func (s *RoomService) UpdateRoomState(ctx context.Context, roomID, ownerUserID, mode string, mediaID *string) (repository.Room, error) {
	room, err := s.rooms.GetRoomByID(ctx, roomID)
	if err != nil {
		return repository.Room{}, err
	}
	if room.OwnerUserID != ownerUserID {
		return repository.Room{}, ErrRoomForbidden
	}
	if room.Status != repository.RoomActive {
		return repository.Room{}, ErrRoomEnded
	}

	var nextMediaID *string
	switch mode {
	case "conference":
		nextMediaID = nil
	case "movie":
		if mediaID == nil || *mediaID == "" {
			return repository.Room{}, ErrMediaRequiredForMovieMode
		}
		media, mediaErr := s.media.GetByID(ctx, *mediaID)
		if mediaErr != nil {
			return repository.Room{}, mediaErr
		}
		if media.OwnerUserID != ownerUserID {
			return repository.Room{}, ErrMediaForbiddenForRoom
		}
		if media.Status != repository.MediaReady {
			return repository.Room{}, ErrMediaNotReady
		}
		nextMediaID = &media.ID
	default:
		return repository.Room{}, ErrInvalidUploadInput
	}

	return s.rooms.UpdateRoomMedia(ctx, roomID, nextMediaID)
}

func (s *RoomService) EndRoom(ctx context.Context, roomID string) (repository.Room, error) {
	room, err := s.rooms.GetRoomByID(ctx, roomID)
	if err != nil {
		return repository.Room{}, err
	}
	if room.Status != repository.RoomActive {
		return room, nil
	}
	endedAt := s.clock()
	if err := s.rooms.EndRoom(ctx, roomID, endedAt); err != nil {
		return repository.Room{}, err
	}
	if err := s.lk.DeleteRoom(ctx, room.Name); err != nil {
		return repository.Room{}, err
	}
	room.Status = repository.RoomEnded
	room.EndedAt = &endedAt
	return room, nil
}

func (s *RoomService) EndStaleActiveRooms(ctx context.Context, minActiveDuration time.Duration) (closedCount int, failedCount int, err error) {
	rooms, err := s.rooms.ListActiveRooms(ctx)
	if err != nil {
		return 0, 0, err
	}
	if minActiveDuration <= 0 {
		minActiveDuration = 24 * time.Hour
	}
	now := s.clock()

	for _, room := range rooms {
		if now.Sub(room.CreatedAt) < minActiveDuration {
			continue
		}
		if _, endErr := s.EndRoom(ctx, room.ID); endErr != nil {
			// Room could be ended by a concurrent request after list/read.
			if errors.Is(endErr, repository.ErrNotFound) {
				continue
			}
			failedCount++
			continue
		}
		closedCount++
	}

	return closedCount, failedCount, nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
