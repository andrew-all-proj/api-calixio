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

type JoinRoomResponse struct {
	RoomID    string `json:"room_id"`
	RoomName  string `json:"room_name"`
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}
