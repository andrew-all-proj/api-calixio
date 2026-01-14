package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAlreadyExists      = errors.New("already exists")
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type User struct {
	ID             string
	Name           string
	Email          string
	EmailConfirmed bool
	PasswordHash   string
	CreatedAt      time.Time
}

type UserRepository interface {
	CreateUser(ctx context.Context, user User) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
}

type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresUserRepository(pool *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

func (r *PostgresUserRepository) CreateUser(ctx context.Context, user User) (User, error) {
	query := `
		INSERT INTO users (id, name, email, email_confirmed, password_hash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, email, email_confirmed, password_hash, created_at
	`
	row := r.pool.QueryRow(ctx, query, user.ID, user.Name, user.Email, user.EmailConfirmed, user.PasswordHash)

	var out User
	if err := row.Scan(&out.ID, &out.Name, &out.Email, &out.EmailConfirmed, &out.PasswordHash, &out.CreatedAt); err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
			return User{}, ErrAlreadyExists
		}
		return User{}, err
	}
	return out, nil
}

func (r *PostgresUserRepository) GetByEmail(ctx context.Context, email string) (User, error) {
	query := `
		SELECT id, name, email, email_confirmed, password_hash, created_at
		FROM users
		WHERE email = $1
	`
	row := r.pool.QueryRow(ctx, query, email)

	var out User
	if err := row.Scan(&out.ID, &out.Name, &out.Email, &out.EmailConfirmed, &out.PasswordHash, &out.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return out, nil
}
