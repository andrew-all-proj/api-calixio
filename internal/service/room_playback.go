package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"calixio/internal/repository"

	"github.com/redis/go-redis/v9"
)

var ErrPlaybackStateNotFound = errors.New("playback state not found")

type PlaybackStatus string

const (
	PlaybackStatusPlaying PlaybackStatus = "playing"
	PlaybackStatusPaused  PlaybackStatus = "paused"
	PlaybackStatusSeeking PlaybackStatus = "seeking"
)

type RoomPlaybackState struct {
	RoomID       string         `json:"roomId"`
	MediaID      string         `json:"mediaId"`
	Status       PlaybackStatus `json:"status"`
	PositionMs   int64          `json:"positionMs"`
	PlaybackRate float64        `json:"playbackRate"`
	UpdatedAt    int64          `json:"updatedAt"`
	Version      int64          `json:"version"`
	HostID       string         `json:"hostId"`
}

type UpdateRoomPlaybackInput struct {
	MediaID      string
	Status       PlaybackStatus
	PositionMs   int64
	PlaybackRate float64
}

type RoomPlaybackService struct {
	rooms repository.RoomRepository
	cache *redis.Client
	clock func() time.Time
}

func NewRoomPlaybackService(rooms repository.RoomRepository, cache *redis.Client) *RoomPlaybackService {
	return &RoomPlaybackService{
		rooms: rooms,
		cache: cache,
		clock: time.Now,
	}
}

func (s *RoomPlaybackService) GetState(ctx context.Context, roomID string) (RoomPlaybackState, error) {
	raw, err := s.cache.Get(ctx, s.stateKey(roomID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return RoomPlaybackState{}, ErrPlaybackStateNotFound
		}
		return RoomPlaybackState{}, err
	}

	var state RoomPlaybackState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return RoomPlaybackState{}, err
	}
	return state, nil
}

func (s *RoomPlaybackService) SaveByHost(ctx context.Context, roomID, hostUserID string, in UpdateRoomPlaybackInput) (RoomPlaybackState, error) {
	room, err := s.rooms.GetRoomByID(ctx, roomID)
	if err != nil {
		return RoomPlaybackState{}, err
	}
	if room.OwnerUserID != hostUserID {
		return RoomPlaybackState{}, ErrRoomForbidden
	}
	if room.Status != repository.RoomActive {
		return RoomPlaybackState{}, ErrRoomEnded
	}

	current, err := s.GetState(ctx, roomID)
	if err != nil && !errors.Is(err, ErrPlaybackStateNotFound) {
		return RoomPlaybackState{}, err
	}
	if errors.Is(err, ErrPlaybackStateNotFound) {
		current = RoomPlaybackState{
			RoomID:       roomID,
			MediaID:      in.MediaID,
			Status:       PlaybackStatusPaused,
			PositionMs:   0,
			PlaybackRate: 1.0,
			UpdatedAt:    s.clock().UnixMilli(),
			Version:      0,
			HostID:       hostUserID,
		}
	}

	if in.MediaID != "" {
		current.MediaID = in.MediaID
	}
	if in.Status != "" {
		current.Status = in.Status
	}
	current.PositionMs = in.PositionMs
	if in.PlaybackRate > 0 {
		current.PlaybackRate = in.PlaybackRate
	} else if current.PlaybackRate <= 0 {
		current.PlaybackRate = 1.0
	}
	current.UpdatedAt = s.clock().UnixMilli()
	current.Version++
	current.HostID = hostUserID

	payload, err := json.Marshal(current)
	if err != nil {
		return RoomPlaybackState{}, err
	}

	if err := s.cache.Set(ctx, s.stateKey(roomID), payload, 24*time.Hour).Err(); err != nil {
		return RoomPlaybackState{}, err
	}
	return current, nil
}

func (s *RoomPlaybackService) stateKey(roomID string) string {
	return "room:playback:v1:" + roomID
}

