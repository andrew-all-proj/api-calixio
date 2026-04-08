package dto

type CreateRoomRequest struct {
	Name string `json:"name" validate:"omitempty,min=2,max=64"`
}

type RoomResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type JoinRoomRequest struct {
	UserName string `json:"user_name" validate:"omitempty,min=1,max=36"`
}

type JoinRoomResponse struct {
	RoomID    string            `json:"room_id"`
	RoomName  string            `json:"room_name"`
	Token     string            `json:"token"`
	ExpiresIn int               `json:"expires_in"`
	State     RoomStateResponse `json:"state"`
}

type RoomStateResponse struct {
	Mode     string                 `json:"mode"`
	MediaID  *string                `json:"media_id,omitempty"`
	Playback *PlaybackMediaResponse `json:"playback,omitempty"`
}

type UpdateRoomStateRequest struct {
	Mode    string  `json:"mode" validate:"required,oneof=conference movie"`
	MediaID *string `json:"media_id,omitempty"`
}

type UpdateRoomStateResponse struct {
	RoomID   string            `json:"room_id"`
	RoomName string            `json:"room_name"`
	State    RoomStateResponse `json:"state"`
}

type RoomPlaybackStateResponse struct {
	RoomID       string  `json:"roomId"`
	MediaID      string  `json:"mediaId"`
	Status       string  `json:"status"`
	PositionMs   int64   `json:"positionMs"`
	PlaybackRate float64 `json:"playbackRate"`
	UpdatedAt    int64   `json:"updatedAt"`
	Version      int64   `json:"version"`
	HostID       string  `json:"hostId"`
}

type UpdateRoomPlaybackRequest struct {
	MediaID      string  `json:"mediaId" validate:"required"`
	Status       string  `json:"status" validate:"required,oneof=playing paused seeking"`
	PositionMs   int64   `json:"positionMs"`
	PlaybackRate float64 `json:"playbackRate" validate:"gt=0"`
}
