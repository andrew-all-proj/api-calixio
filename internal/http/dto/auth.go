package dto

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type LoginResponse struct {
	Name         string `json:"name"`
	AccessToken  string `json:"access_token"`
	AccessTTL    int    `json:"access_ttl"`
	RefreshTTL   int    `json:"refresh_ttl"`
}

type RegisterRequest struct {
	Name     string `json:"name" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

type RegisterResponse struct {
	UserID string `json:"user_id"`
}

type RefreshRequest struct{}

type RefreshResponse struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	AccessTTL    int    `json:"access_ttl"`
	RefreshTTL   int    `json:"refresh_ttl"`
	Version      int    `json:"version"`
}
