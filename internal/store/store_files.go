package store

import (
	"context"

	"github.com/aitoys/llm-gateway/internal/model"
)

func (s *Store) CreateFile(ctx context.Context, f *model.File) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO files(id,tenant_id,user_id,filename,mime_type,size,storage_url,purpose,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		f.ID, f.TenantID, f.UserID, f.Filename, f.MimeType, f.Size, f.StorageURL, f.Purpose, f.CreatedAt)
	return err
}
