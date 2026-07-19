package store

import (
	"context"
	"errors"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// nonNilStrSlice 确保 text[] 列收到空数组而非 NULL(NOT NULL 约束)。
func nonNilStrSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Store) CreateAPIKey(ctx context.Context, k *model.APIKey) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO api_keys(id,tenant_id,user_id,key_prefix,key_hash,name,scopes,models,rpm_limit,tpm_limit,daily_request_limit,monthly_request_limit,daily_token_limit,monthly_token_limit,ip_whitelist,expires_at,status,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		k.ID, k.TenantID, k.UserID, k.KeyPrefix, k.KeyHash, k.Name, nonNilStrSlice(k.Scopes), nonNilStrSlice(k.Models),
		k.RPMLimit, k.TPMLimit, k.DailyRequestLimit, k.MonthlyRequestLimit, k.DailyTokenLimit, k.MonthlyTokenLimit,
		nonNilStrSlice(k.IPWhitelist), k.ExpiresAt, k.Status, k.CreatedAt)
	return err
}

// GetAPIKeyByHash 按 hash 查询(用于鉴权)。
func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*model.APIKey, error) {
	k := &model.APIKey{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id,tenant_id,user_id,key_prefix,key_hash,name,scopes,models,rpm_limit,tpm_limit,daily_request_limit,monthly_request_limit,daily_token_limit,monthly_token_limit,ip_whitelist,expires_at,last_used_at,status,created_at
		 FROM api_keys WHERE key_hash=$1 AND status='active'`, hash).
		Scan(&k.ID, &k.TenantID, &k.UserID, &k.KeyPrefix, &k.KeyHash, &k.Name, &k.Scopes, &k.Models,
			&k.RPMLimit, &k.TPMLimit, &k.DailyRequestLimit, &k.MonthlyRequestLimit, &k.DailyTokenLimit, &k.MonthlyTokenLimit, &k.IPWhitelist, &k.ExpiresAt, &k.LastUsedAt, &k.Status, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Store) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE api_keys SET last_used_at=$2 WHERE id=$1`, id, time.Now())
	return err
}

// GetAPIKeyByID 按 id 取密钥(含 models 白名单,供 /v1/models 过滤)。
func (s *Store) GetAPIKeyByID(ctx context.Context, id string) (*model.APIKey, error) {
	k := &model.APIKey{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id,tenant_id,user_id,key_prefix,key_hash,name,scopes,models,rpm_limit,tpm_limit,daily_request_limit,monthly_request_limit,daily_token_limit,monthly_token_limit,ip_whitelist,expires_at,last_used_at,status,created_at
		 FROM api_keys WHERE id=$1`, id).
		Scan(&k.ID, &k.TenantID, &k.UserID, &k.KeyPrefix, &k.KeyHash, &k.Name, &k.Scopes, &k.Models,
			&k.RPMLimit, &k.TPMLimit, &k.DailyRequestLimit, &k.MonthlyRequestLimit, &k.DailyTokenLimit, &k.MonthlyTokenLimit, &k.IPWhitelist, &k.ExpiresAt, &k.LastUsedAt, &k.Status, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, userID string) ([]*model.APIKey, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id,tenant_id,user_id,key_prefix,key_hash,name,scopes,models,rpm_limit,tpm_limit,daily_request_limit,monthly_request_limit,daily_token_limit,monthly_token_limit,ip_whitelist,expires_at,last_used_at,status,created_at
		 FROM api_keys WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.APIKey
	for rows.Next() {
		k := &model.APIKey{}
		if err := rows.Scan(&k.ID, &k.TenantID, &k.UserID, &k.KeyPrefix, &k.KeyHash, &k.Name, &k.Scopes, &k.Models,
			&k.RPMLimit, &k.TPMLimit, &k.DailyRequestLimit, &k.MonthlyRequestLimit, &k.DailyTokenLimit, &k.MonthlyTokenLimit, &k.IPWhitelist, &k.ExpiresAt, &k.LastUsedAt, &k.Status, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey 吊销密钥并返回其 hash(供调用方清理鉴权缓存)。
func (s *Store) RevokeAPIKey(ctx context.Context, id, userID string) (string, error) {
	var hash string
	err := s.Pool.QueryRow(ctx,
		`UPDATE api_keys SET status='revoked' WHERE id=$1 AND user_id=$2 RETURNING key_hash`, id, userID).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return hash, nil
}

// RevokeAPIKeyScoped 按租户 scope 吊销密钥(租户管理员吊销本租户任意成员的 key,不限 user_id)。
// 返回 key_hash 供调用方清鉴权缓存;非本租户或不存在返回 ErrNotFound(不泄露是否存在)。
func (s *Store) RevokeAPIKeyScoped(ctx context.Context, id, tenantID string) (string, error) {
	var hash string
	err := s.Pool.QueryRow(ctx,
		`UPDATE api_keys SET status='revoked' WHERE id=$1 AND tenant_id=$2 RETURNING key_hash`, id, tenantID).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return hash, nil
}

// RevokeAPIKeyAny 按 id 吊销密钥(不限租户,供平台管理员);返回 hash 供清缓存。
func (s *Store) RevokeAPIKeyAny(ctx context.Context, id string) (string, error) {
	var hash string
	err := s.Pool.QueryRow(ctx,
		`UPDATE api_keys SET status='revoked' WHERE id=$1 RETURNING key_hash`, id).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return hash, nil
}

// TenantAPIKey 租户视角的密钥行(附属主用户邮箱,便于管理员识别归属与统一吊销)。
type TenantAPIKey struct {
	ID                  string     `json:"id"`
	UserID              string     `json:"user_id"`
	UserEmail           string     `json:"user_email"`
	Name                string     `json:"name"` // 密钥自身名称(如"生产环境")
	KeyPrefix           string     `json:"key_prefix"`
	RPMLimit            int        `json:"rpm_limit"`
	TPMLimit            int        `json:"tpm_limit"`
	DailyRequestLimit   int        `json:"daily_request_limit"`
	MonthlyRequestLimit int        `json:"monthly_request_limit"`
	IPWhitelist         []string   `json:"ip_whitelist"`
	Status              string     `json:"status"`
	LastUsedAt          *time.Time `json:"last_used_at"`
	CreatedAt           time.Time  `json:"created_at"`
}

// ListAPIKeysByTenant 列出某租户下全部用户的密钥(JOIN users 带邮箱,供租户管理员统一管控)。
// platform 管理员传 tenantFilter="" 时返回全租户。
func (s *Store) ListAPIKeysByTenant(ctx context.Context, tenantFilter string) ([]*TenantAPIKey, error) {
	q := `SELECT k.id, k.user_id, u.email, k.name, k.key_prefix,
	             k.rpm_limit, k.tpm_limit, k.daily_request_limit, k.monthly_request_limit,
	             k.ip_whitelist, k.status, k.last_used_at, k.created_at
	      FROM api_keys k JOIN users u ON u.id = k.user_id`
	args := []any{}
	if tenantFilter != "" {
		q += " WHERE k.tenant_id=$1"
		args = append(args, tenantFilter)
	}
	q += " ORDER BY k.created_at DESC"
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TenantAPIKey
	for rows.Next() {
		k := &TenantAPIKey{}
		if err := rows.Scan(&k.ID, &k.UserID, &k.UserEmail, &k.Name, &k.KeyPrefix,
			&k.RPMLimit, &k.TPMLimit, &k.DailyRequestLimit, &k.MonthlyRequestLimit,
			&k.IPWhitelist, &k.Status, &k.LastUsedAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// APIKeyHashesByUser 返回某用户全部 active 密钥的 hash(用于禁用用户时批量清理鉴权缓存)。
func (s *Store) APIKeyHashesByUser(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT key_hash FROM api_keys WHERE user_id=$1 AND status='active'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
