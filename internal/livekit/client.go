package livekit

import (
	"context"
	"net/http"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type Client struct {
	apiKey    string
	apiSecret string
	roomSvc   *lksdk.RoomServiceClient
	keyProv   auth.KeyProvider

	roomAutoTimeout time.Duration
	tokenTTL        time.Duration
}

func NewClient(host, apiKey, apiSecret, webhookSecret string, roomAutoTimeout, tokenTTL time.Duration) *Client {
	roomSvc := lksdk.NewRoomServiceClient(host, apiKey, apiSecret)
	secret := webhookSecret
	if secret == "" {
		secret = apiSecret
	}
	return &Client{
		apiKey:          apiKey,
		apiSecret:       apiSecret,
		roomSvc:         roomSvc,
		keyProv:         auth.NewSimpleKeyProvider(apiKey, secret),
		roomAutoTimeout: roomAutoTimeout,
		tokenTTL:        tokenTTL,
	}
}

func (c *Client) CreateRoom(ctx context.Context, name string) error {
	_, err := c.roomSvc.CreateRoom(ctx, &livekit.CreateRoomRequest{
		Name:            name,
		EmptyTimeout:    uint32(c.roomAutoTimeout.Seconds()),
		MaxParticipants: 16,
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) DeleteRoom(ctx context.Context, name string) error {
	_, err := c.roomSvc.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: name})
	return err
}

func (c *Client) GenerateToken(identity, roomName string) (string, error) {
	canPublish := boolPtr(true)
	canSubscribe := boolPtr(true)
	grant := &auth.VideoGrant{
		Room:         roomName,
		RoomJoin:     true,
		CanPublish:   canPublish,
		CanSubscribe: canSubscribe,
	}

	token := auth.NewAccessToken(c.apiKey, c.apiSecret)
	token.SetIdentity(identity)
	token.SetVideoGrant(grant)
	token.SetValidFor(c.tokenTTL)
	return token.ToJWT()
}

func (c *Client) ReceiveWebhook(r *http.Request) (*livekit.WebhookEvent, error) {
	return webhook.ReceiveWebhookEvent(r, c.keyProv)
}

func boolPtr(v bool) *bool {
	return &v
}
