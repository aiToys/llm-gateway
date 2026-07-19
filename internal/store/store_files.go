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

func (s *Store) GetFile(ctx context.Context, id string) (*model.File, error) {
	f := &model.File{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id,tenant_id,user_id,filename,mime_type,size,storage_url,purpose,created_at FROM files WHERE id=$1`, id).
		Scan(&f.ID, &f.TenantID, &f.UserID, &f.Filename, &f.MimeType, &f.Size, &f.StorageURL, &f.Purpose, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return f, nil
}
