package store

import (
	"context"
	"fmt"

	"github.com/aitoys/llm-gateway/internal/model"
)

// --- tenants ---

func (s *Store) CreateTenant(ctx context.Context, t *model.Tenant) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO tenants(id,name,slug,status,created_at) VALUES($1,$2,$3,$4,$5)`,
		t.ID, t.Name, t.Slug, t.Status, t.CreatedAt)
	return err
}

func (s *Store) GetTenant(ctx context.Context, id string) (*model.Tenant, error) {
	t := &model.Tenant{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id,name,slug,status,created_at FROM tenants WHERE id=$1`, id).
		Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]*model.Tenant, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,name,slug,status,created_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Tenant
	for rows.Next() {
		t := &model.Tenant{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateTenant 更新租户可编辑字段(名称 / Slug)。
func (s *Store) UpdateTenant(ctx context.Context, id, name, slug string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE tenants SET name=$2, slug=$3 WHERE id=$1`, id, name, slug)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found")
	}
	return nil
}

// SetTenantStatus 设置租户状态(active | disabled)。
func (s *Store) SetTenantStatus(ctx context.Context, id, status string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE tenants SET status=$2 WHERE id=$1`, id, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found")
	}
	return nil
}

// --- users ---

func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO users(id,tenant_id,email,password_hash,role,status,balance_cents,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		u.ID, u.TenantID, u.Email, u.PasswordHash, u.Role, u.Status, u.BalanceCents, u.CreatedAt)
	return err
}

func (s *Store) GetUser(ctx context.Context, id string) (*model.User, error) {
	u := &model.User{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id,tenant_id,email,password_hash,role,status,balance_cents,created_at FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.BalanceCents, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, tenantID, email string) (*model.User, error) {
	u := &model.User{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id,tenant_id,email,password_hash,role,status,balance_cents,created_at
		 FROM users WHERE tenant_id=$1 AND email=$2`, tenantID, email).
		Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.BalanceCents, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context, tenantID string) ([]*model.User, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id,tenant_id,email,password_hash,role,status,balance_cents,created_at
		 FROM users WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u := &model.User{}
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.BalanceCents, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ListAllUsers 跨租户列出全部用户(管理端)。
func (s *Store) ListAllUsers(ctx context.Context) ([]*model.User, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id,tenant_id,email,password_hash,role,status,balance_cents,created_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u := &model.User{}
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.BalanceCents, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserStatus(ctx context.Context, id, status string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE users SET status=$2 WHERE id=$1`, id, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// UpdateUser 更新用户邮箱与角色(不改密码/余额)。
func (s *Store) UpdateUser(ctx context.Context, id, email, role string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE users SET email=$2, role=$3 WHERE id=$1`, id, email, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// SetUserPassword 重置用户密码哈希。
func (s *Store) SetUserPassword(ctx context.Context, id, passwordHash string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, id, passwordHash)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

