ALTER TABLE files
    DROP COLUMN IF EXISTS is_active,
    DROP COLUMN IF EXISTS updated_at;