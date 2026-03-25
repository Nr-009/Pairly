package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UpdateFile updates the name and language of a file.
// Returns the updated file so the caller has the new values.
func UpdateFile(ctx context.Context, db *pgxpool.Pool, fileID, name, language string) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		UPDATE files
		SET name = $1, language = $2, updated_at = NOW()
		WHERE id = $3 AND is_active = true
		RETURNING id, room_id, name, language, is_active, created_by, created_at, updated_at
	`, name, language, fileID).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.IsActive, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update file: %w", err)
	}
	return f, nil
}

// GetFileByID fetches a single active file by ID.
func GetFileByID(ctx context.Context, db *pgxpool.Pool, fileID string) (*File, error) {
	f := &File{}
	err := db.QueryRow(ctx, `
		SELECT id, room_id, name, language, is_active, created_by, created_at, updated_at
		FROM files
		WHERE id = $1 AND is_active = true
	`, fileID).Scan(
		&f.ID, &f.RoomID, &f.Name, &f.Language,
		&f.IsActive, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}
	return f, nil
}

// fileUpdatedAt formats updated_at safely since it's nullable
func (f *File) UpdatedAtStr() string {
	if f.UpdatedAt == nil {
		return ""
	}
	return f.UpdatedAt.Format(time.RFC3339)
}
