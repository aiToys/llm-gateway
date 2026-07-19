package store

import (
	"context"
	"errors"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// CreateInvite 新建邀请令牌(存 hash,明文由调用方持有并一次性返回)。
func (s *Store) CreateInvite(ctx context.Context, t *model.InviteToken) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO invite_tokens(id,token_hash,tenant_id,role,created_by,expires_at,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7)`,
		t.ID, t.TokenHash, t.TenantID, t.Role, t.CreatedBy, t.ExpiresAt, t.CreatedAt)
	return err
}

const inviteCols = `id,token_hash,tenant_id,role,created_by,expires_at,used_at,used_by,created_at`

// GetInviteByToken 按 token hash 查邀请。
func (s *Store) GetInviteByToken(ctx context.Context, tokenHash string) (*model.InviteToken, error) {
	return scanInvite(s.Pool.QueryRow(ctx,
		`SELECT `+inviteCols+` FROM invite_tokens WHERE token_hash=$1`, tokenHash))
}

// ListInvites 列出某租户的邀请(最新在前)。
func (s *Store) ListInvites(ctx context.Context, tenantID string) ([]*model.InviteToken, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+inviteCols+` FROM invite_tokens WHERE tenant_id=$1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.InviteToken
	for rows.Next() {
		t, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AcceptInviteAtomic 在单事务内"认领邀请(CAS) + 创建成员账号",杜绝并发重复接受/越权入伙。
// CAS 门票: UPDATE ... WHERE token_hash=$ AND used_at IS NULL AND expires_at>$2 RETURNING。
// RowsAffected=0 表示已用/过期/不存在 → 返回 ErrInviteUnavailable,调用方拒绝。
// (tenant_id,email) 唯一约束兜底: 同邮箱跨租户重复插入会失败回滚,邀请也不被消耗。
func (s *Store) AcceptInviteAtomic(ctx context.Context, tokenHash, email, pwHash string, now time.Time) (*model.InviteToken, string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback(ctx)

	inv := &model.InviteToken{}
	err = tx.QueryRow(ctx,
		`UPDATE invite_tokens SET used_at=$2
		 WHERE token_hash=$1 AND used_at IS NULL AND expires_at>$2
		 RETURNING id,tenant_id,role,expires_at,created_by`, tokenHash, now).
		Scan(&inv.ID, &inv.TenantID, &inv.Role, &inv.ExpiresAt, &inv.CreatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrInviteUnavailable
		}
		return nil, "", err
	}
	userID := storeID()
	if _, err = tx.Exec(ctx,
		`INSERT INTO users(id,tenant_id,email,password_hash,role,status,balance_cents,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		userID, inv.TenantID, email, pwHash, inv.Role, "active", 0, now); err != nil {
		return nil, "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE invite_tokens SET used_by=$2 WHERE id=$1`, inv.ID, userID); err != nil {
		return nil, "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return inv, userID, nil
}

// ErrInviteUnavailable 邀请不存在/已使用/已过期。
var ErrInviteUnavailable = errors.New("invite unavailable, used, or expired")

// RevokeInvite 删除邀请令牌(吊销)。
func (s *Store) RevokeInvite(ctx context.Context, id, tenantID string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM invite_tokens WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("invite not found")
	}
	return nil
}

type inviteScannable interface {
	Scan(dest ...any) error
}

func scanInvite(row inviteScannable) (*model.InviteToken, error) {
	t := &model.InviteToken{}
	if err := row.Scan(&t.ID, &t.TokenHash, &t.TenantID, &t.Role, &t.CreatedBy,
		&t.ExpiresAt, &t.UsedAt, &t.UsedBy, &t.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}
