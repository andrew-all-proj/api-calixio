-- +goose Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL UNIQUE,
  email_confirmed BOOLEAN NOT NULL DEFAULT false,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_created_at()
RETURNS TRIGGER AS $$
BEGIN
  IF NEW.created_at IS NULL THEN
    NEW.created_at := now();
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS users_set_created_at ON users;
CREATE TRIGGER users_set_created_at
BEFORE INSERT ON users
FOR EACH ROW EXECUTE FUNCTION set_created_at();

CREATE TABLE IF NOT EXISTS sessions (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash TEXT NOT NULL UNIQUE,
  access_jti TEXT NOT NULL UNIQUE,
  version INTEGER NOT NULL DEFAULT 1,
  issued_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS sessions_user_idx ON sessions(user_id);
CREATE INDEX IF NOT EXISTS sessions_access_jti_idx ON sessions(access_jti);

CREATE TYPE room_status AS ENUM ('pending', 'active', 'ended');

CREATE TABLE IF NOT EXISTS rooms (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_by TEXT NOT NULL,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status room_status NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS rooms_status_idx ON rooms(status);

-- +goose Down
DROP INDEX IF EXISTS rooms_status_idx;
DROP TABLE IF EXISTS rooms;
DROP TYPE IF EXISTS room_status;
DROP TABLE IF EXISTS sessions;
DROP TRIGGER IF EXISTS users_set_created_at ON users;
DROP FUNCTION IF EXISTS set_created_at();
DROP TABLE IF EXISTS users;
