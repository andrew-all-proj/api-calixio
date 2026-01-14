package service

import (
	"context"
	"fmt"

	"github.com/livekit/protocol/livekit"
	"github.com/redis/go-redis/v9"
)

type WebhookService struct {
	redis *redis.Client
}

func NewWebhookService(redis *redis.Client) *WebhookService {
	return &WebhookService{redis: redis}
}

func (s *WebhookService) HandleEvent(ctx context.Context, event *livekit.WebhookEvent) error {
	if event == nil {
		return nil
	}

	switch event.Event {
	case "participant_joined":
		return s.trackParticipant(ctx, event.Room.GetName(), event.Participant.GetIdentity(), true)
	case "participant_left":
		return s.trackParticipant(ctx, event.Room.GetName(), event.Participant.GetIdentity(), false)
	default:
		return nil
	}
}

func (s *WebhookService) trackParticipant(ctx context.Context, roomName, identity string, join bool) error {
	if roomName == "" || identity == "" {
		return nil
	}
	key := fmt.Sprintf("room:%s:participants", roomName)
	if join {
		return s.redis.SAdd(ctx, key, identity).Err()
	}
	return s.redis.SRem(ctx, key, identity).Err()
}
