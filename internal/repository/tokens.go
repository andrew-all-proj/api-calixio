package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID               string
	UserID           string
	RefreshTokenHash string
	AccessJTI        string
	Version          int
	IssuedAt         time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

type SessionRepository interface {
	Store(ctx context.Context, session Session) error
	GetByRefreshTokenHash(ctx context.Context, hash string) (Session, error)
	Rotate(ctx context.Context, sessionID, newAccessJTI, newRefreshHash string, issuedAt, expiresAt time.Time) (Session, error)
	IsAccessRevoked(ctx context.Context, accessJTI string) (bool, error)
	RevokeByAccessJTI(ctx context.Context, accessJTI string, revokedAt time.Time) error
}

type PostgresSessionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresSessionRepository(pool *pgxpool.Pool) *PostgresSessionRepository {
	return &PostgresSessionRepository{pool: pool}
}

func (r *PostgresSessionRepository) Store(ctx context.Context, session Session) error {
	query := `
		INSERT INTO sessions (id, user_id, refresh_token_hash, access_jti, version, issued_at, expires_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.pool.Exec(ctx, query, session.ID, session.UserID, session.RefreshTokenHash, session.AccessJTI, session.Version, session.IssuedAt, session.ExpiresAt, session.RevokedAt)
	return err
}

func (r *PostgresSessionRepository) GetByRefreshTokenHash(ctx context.Context, hash string) (Session, error) {
	query := `
		SELECT id, user_id, refresh_token_hash, access_jti, version, issued_at, expires_at, revoked_at
		FROM sessions
		WHERE refresh_token_hash = $1
	`
	var out Session
	if err := r.pool.QueryRow(ctx, query, hash).Scan(
		&out.ID,
		&out.UserID,
		&out.RefreshTokenHash,
		&out.AccessJTI,
		&out.Version,
		&out.IssuedAt,
		&out.ExpiresAt,
		&out.RevokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, err
	}
	return out, nil
}

func (r *PostgresSessionRepository) Rotate(ctx context.Context, sessionID, newAccessJTI, newRefreshHash string, issuedAt, expiresAt time.Time) (Session, error) {
	query := `
		UPDATE sessions
		SET refresh_token_hash = $2,
			access_jti = $3,
			issued_at = $4,
			expires_at = $5,
			version = version + 1
		WHERE id = $1 AND revoked_at IS NULL
		RETURNING id, user_id, refresh_token_hash, access_jti, version, issued_at, expires_at, revoked_at
	`
	var out Session
	if err := r.pool.QueryRow(ctx, query, sessionID, newRefreshHash, newAccessJTI, issuedAt, expiresAt).Scan(
		&out.ID,
		&out.UserID,
		&out.RefreshTokenHash,
		&out.AccessJTI,
		&out.Version,
		&out.IssuedAt,
		&out.ExpiresAt,
		&out.RevokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, err
	}
	return out, nil
}

func (r *PostgresSessionRepository) IsAccessRevoked(ctx context.Context, accessJTI string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM sessions WHERE access_jti = $1 AND revoked_at IS NOT NULL
		)
	`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, accessJTI).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *PostgresSessionRepository) RevokeByAccessJTI(ctx context.Context, accessJTI string, revokedAt time.Time) error {
	query := `
		UPDATE sessions
		SET revoked_at = $2
		WHERE access_jti = $1 AND revoked_at IS NULL
	`
	_, err := r.pool.Exec(ctx, query, accessJTI, revokedAt)
	return err
}
