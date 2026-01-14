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

type RoomService struct {
	rooms repository.RoomRepository
	lk    *livekit.Client
	clock func() time.Time
}

func NewRoomService(rooms repository.RoomRepository, lk *livekit.Client) *RoomService {
	return &RoomService{rooms: rooms, lk: lk, clock: time.Now}
}

type CreateRoomInput struct {
	Name      string
	CreatedBy string
	UserID    string
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
		ID:        roomID,
		Name:      name,
		CreatedBy: in.CreatedBy,
		UserID:    in.UserID,
		Status:    repository.RoomActive,
		CreatedAt: s.clock(),
	}
	return s.rooms.CreateRoom(ctx, room)
}

func (s *RoomService) JoinRoom(ctx context.Context, roomID, identity string) (string, repository.Room, error) {
	room, err := s.rooms.GetRoomByID(ctx, roomID)
	if err != nil {
		return "", repository.Room{}, err
	}
	if room.Status != repository.RoomActive {
		return "", repository.Room{}, ErrRoomEnded
	}
	jwt, err := s.lk.GenerateToken(identity, room.Name)
	if err != nil {
		return "", repository.Room{}, err
	}
	return jwt, room, nil
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

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
