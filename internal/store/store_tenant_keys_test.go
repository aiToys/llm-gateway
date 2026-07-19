package store

import (
	"context"
	"testing"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
)

// TestListAPIKeysByTenant 验证租户级 key 列表只返回本租户,且带出用户邮箱。
func TestListAPIKeysByTenant(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-tk-" + storeID()
	other := "t-tk-" + storeID()
	uid := seedUser(t, s, tenant, 0)
	uidOther := seedUser(t, s, other, 0)

	mk := func(uid, tenant string) *model.APIKey {
		return &model.APIKey{
			ID: storeID(), TenantID: tenant, UserID: uid, KeyPrefix: "sk-x", KeyHash: storeID(),
			Name: "k", Scopes: []string{"chat"}, Status: "active", CreatedAt: time.Now(),
		}
	}
	if err := s.CreateAPIKey(ctx, mk(uid, tenant)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAPIKey(ctx, mk(uidOther, other)); err != nil {
		t.Fatal(err)
	}

	// 本租户列表仅 1 条,且带出邮箱。
	got, err := s.ListAPIKeysByTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 key got %d", len(got))
	}
	if got[0].UserEmail == "" {
		t.Fatalf("user email not joined: %+v", got[0])
	}

	// 跨租户吊销应失败(ErrNotFound,不泄露存在)。
	if _, err := s.RevokeAPIKeyScoped(ctx, got[0].ID, other); err == nil {
		t.Fatal("cross-tenant revoke must fail")
	}
	// 本租户吊销成功。
	if _, err := s.RevokeAPIKeyScoped(ctx, got[0].ID, tenant); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// 重复吊销幂等(不报错,已 revoked 再设无副作用)。
	if _, err := s.RevokeAPIKeyScoped(ctx, got[0].ID, tenant); err != nil {
		t.Fatalf("re-revoke should be idempotent: %v", err)
	}
}
