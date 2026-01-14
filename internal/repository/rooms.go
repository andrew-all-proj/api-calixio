package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Room struct {
	ID        string
	Name      string
	CreatedBy string
	UserID    string
	Status    RoomStatus
	CreatedAt time.Time
	EndedAt   *time.Time
}

type RoomStatus string

const (
	RoomPending RoomStatus = "pending"
	RoomActive  RoomStatus = "active"
	RoomEnded   RoomStatus = "ended"
)

type RoomRepository interface {
	CreateRoom(ctx context.Context, room Room) (Room, error)
	GetRoomByID(ctx context.Context, id string) (Room, error)
	EndRoom(ctx context.Context, id string, endedAt time.Time) error
}

type PostgresRoomRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRoomRepository(pool *pgxpool.Pool) *PostgresRoomRepository {
	return &PostgresRoomRepository{pool: pool}
}

func (r *PostgresRoomRepository) CreateRoom(ctx context.Context, room Room) (Room, error) {
	query := `
		INSERT INTO rooms (id, name, created_by, user_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, created_by, user_id, status, created_at, ended_at
	`
	row := r.pool.QueryRow(ctx, query, room.ID, room.Name, room.CreatedBy, room.UserID, string(room.Status), room.CreatedAt)

	var out Room
	var status string
	err := row.Scan(&out.ID, &out.Name, &out.CreatedBy, &out.UserID, &status, &out.CreatedAt, &out.EndedAt)
	if err != nil {
		return Room{}, err
	}
	out.Status = RoomStatus(status)
	return out, nil
}

func (r *PostgresRoomRepository) GetRoomByID(ctx context.Context, id string) (Room, error) {
	query := `
		SELECT id, name, created_by, user_id, status, created_at, ended_at
		FROM rooms
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var out Room
	var status string
	if err := row.Scan(&out.ID, &out.Name, &out.CreatedBy, &out.UserID, &status, &out.CreatedAt, &out.EndedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Room{}, ErrNotFound
		}
		return Room{}, err
	}
	out.Status = RoomStatus(status)
	return out, nil
}

func (r *PostgresRoomRepository) EndRoom(ctx context.Context, id string, endedAt time.Time) error {
	query := `
		UPDATE rooms
		SET status = 'ended', ended_at = $2
		WHERE id = $1 AND status = 'active'
	`
	ct, err := r.pool.Exec(ctx, query, id, endedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
