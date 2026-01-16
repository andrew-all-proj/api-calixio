package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"calixio/internal/http/authn"
	"calixio/internal/repository"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	users      repository.UserRepository
	sessions   repository.SessionRepository
	jwt        *authn.JWTService
	accessTTL  time.Duration
	refreshTTL time.Duration
	clock      func() time.Time
}

func NewAuthService(users repository.UserRepository, sessions repository.SessionRepository, jwt *authn.JWTService, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{
		users:      users,
		sessions:   sessions,
		jwt:        jwt,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		clock:      time.Now,
	}
}

type RegisterInput struct {
	Name     string
	Email    string
	Password string
}

type RegisterResult struct {
	UserID string
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (RegisterResult, error) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return RegisterResult{}, err
	}

	user := repository.User{
		ID:             uuid.NewString(),
		Name:           in.Name,
		Email:          in.Email,
		EmailConfirmed: false,
		PasswordHash:   string(passwordHash),
	}
	created, err := s.users.CreateUser(ctx, user)
	if err != nil {
		return RegisterResult{}, err
	}

	return RegisterResult{
		UserID: created.ID,
	}, nil
}

type RefreshResult struct {
	UserID       string
	AccessToken  string
	RefreshToken string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	Version      int
}

type LoginInput struct {
	Email    string
	Password string
}

type LoginResult struct {
	UserID       string
	Name         string
	AccessToken  string
	RefreshToken string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
}

func (s *AuthService) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	user, err := s.users.GetByEmail(ctx, in.Email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return LoginResult{}, repository.ErrInvalidCredentials
		}
		return LoginResult{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)); err != nil {
		return LoginResult{}, repository.ErrInvalidCredentials
	}

	accessToken, accessJTI, err := s.jwt.IssueToken(user.ID, user.Name)
	if err != nil {
		return LoginResult{}, err
	}

	refreshRaw, refreshHash, err := generateRefreshToken()
	if err != nil {
		return LoginResult{}, err
	}

	now := s.clock()
	session := repository.Session{
		ID:               uuid.NewString(),
		UserID:           user.ID,
		RefreshTokenHash: refreshHash,
		AccessJTI:        accessJTI,
		Version:          1,
		IssuedAt:         now,
		ExpiresAt:        now.Add(s.refreshTTL),
	}
	if err := s.sessions.Store(ctx, session); err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		UserID:       user.ID,
		Name:         user.Name,
		AccessToken:  accessToken,
		RefreshToken: refreshRaw,
		AccessTTL:    s.accessTTL,
		RefreshTTL:   s.refreshTTL,
	}, nil
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (RefreshResult, error) {
	refreshHash := hashToken(refreshToken)
	session, err := s.sessions.GetByRefreshTokenHash(ctx, refreshHash)
	if err != nil {
		return RefreshResult{}, err
	}
	if session.RevokedAt != nil || session.ExpiresAt.Before(s.clock()) {
		return RefreshResult{}, repository.ErrSessionNotFound
	}

	accessToken, accessJTI, err := s.jwt.IssueToken(session.UserID, "")
	if err != nil {
		return RefreshResult{}, err
	}
	newRefreshRaw, newRefreshHash, err := generateRefreshToken()
	if err != nil {
		return RefreshResult{}, err
	}

	now := s.clock()
	updated, err := s.sessions.Rotate(ctx, session.ID, accessJTI, newRefreshHash, now, now.Add(s.refreshTTL))
	if err != nil {
		return RefreshResult{}, err
	}

	return RefreshResult{
		UserID:       updated.UserID,
		AccessToken:  accessToken,
		RefreshToken: newRefreshRaw,
		AccessTTL:    s.accessTTL,
		RefreshTTL:   s.refreshTTL,
		Version:      updated.Version,
	}, nil
}

func generateRefreshToken() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw := hex.EncodeToString(b)
	hash := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(hash[:]), nil
}

func hashToken(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}
