-- +goose Up
CREATE TABLE IF NOT EXISTS media (
  id TEXT PRIMARY KEY,
  owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  original_name TEXT NOT NULL,
  storage_key TEXT NOT NULL UNIQUE,
  playback_url TEXT NOT NULL,
  preview_url TEXT,
  duration_sec INTEGER,
  file_size_bytes BIGINT NOT NULL,
  mime_type TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS media_owner_user_idx ON media(owner_user_id);
CREATE INDEX IF NOT EXISTS media_status_idx ON media(status);
CREATE INDEX IF NOT EXISTS media_deleted_at_idx ON media(deleted_at);

ALTER TABLE rooms
  ADD COLUMN IF NOT EXISTS owner_user_id UUID,
  ADD COLUMN IF NOT EXISTS media_id TEXT;

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_name = 'rooms' AND column_name = 'user_id'
  ) THEN
    EXECUTE 'UPDATE rooms SET owner_user_id = user_id WHERE owner_user_id IS NULL';
  END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE rooms
  ALTER COLUMN owner_user_id SET NOT NULL;

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'rooms_owner_user_id_fkey'
  ) THEN
    ALTER TABLE rooms
      ADD CONSTRAINT rooms_owner_user_id_fkey
      FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE;
  END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'rooms_media_id_fkey'
  ) THEN
    ALTER TABLE rooms
      ADD CONSTRAINT rooms_media_id_fkey
      FOREIGN KEY (media_id) REFERENCES media(id) ON DELETE SET NULL;
  END IF;
END;
$$;
-- +goose StatementEnd

DROP INDEX IF EXISTS rooms_user_idx;
CREATE INDEX IF NOT EXISTS rooms_owner_user_idx ON rooms(owner_user_id);
CREATE INDEX IF NOT EXISTS rooms_media_idx ON rooms(media_id);

ALTER TABLE rooms DROP COLUMN IF EXISTS created_by;
ALTER TABLE rooms DROP COLUMN IF EXISTS user_id;

-- +goose Down
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS created_by TEXT;
ALTER TABLE rooms ADD COLUMN IF NOT EXISTS user_id UUID;

UPDATE rooms
SET created_by = owner_user_id::text,
    user_id = owner_user_id
WHERE owner_user_id IS NOT NULL;

DROP INDEX IF EXISTS rooms_media_idx;
DROP INDEX IF EXISTS rooms_owner_user_idx;

ALTER TABLE rooms DROP CONSTRAINT IF EXISTS rooms_media_id_fkey;
ALTER TABLE rooms DROP CONSTRAINT IF EXISTS rooms_owner_user_id_fkey;
ALTER TABLE rooms DROP COLUMN IF EXISTS media_id;
ALTER TABLE rooms DROP COLUMN IF EXISTS owner_user_id;

DROP INDEX IF EXISTS media_deleted_at_idx;
DROP INDEX IF EXISTS media_status_idx;
DROP INDEX IF EXISTS media_owner_user_idx;
DROP TABLE IF EXISTS media;
