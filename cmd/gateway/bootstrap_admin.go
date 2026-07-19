package main

import (
	"context"
	"log"
	"time"

	"github.com/aitoys/llm-gateway/internal/config"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/google/uuid"
)

// ensureBootstrapAdmin 在启动时确保配置声明的平台超级管理员存在。
// 配置 auth.bootstrap_admin.{email,password} 均非空时生效;已存在同 email 则跳过。
// 用于首次生产部署:自助注册只能得到租户级 admin,平台跨租户管理权需由此引导。
func ensureBootstrapAdmin(st *store.Store, cfg *config.Config) error {
	ba := cfg.Auth.BootstrapAdmin
	if ba.Email == "" || ba.Password == "" {
		return nil
	}
	ctx := context.Background()

	// 确保平台内置租户存在。
	if _, err := st.GetTenant(ctx, model.PlatformTenantID); err != nil {
		if err := st.CreateTenant(ctx, &model.Tenant{
			ID: model.PlatformTenantID, Name: "平台管理", Slug: "platform", Status: "active", CreatedAt: time.Now(),
		}); err != nil {
			return err
		}
	}

	if _, err := st.GetUserByEmail(ctx, model.PlatformTenantID, ba.Email); err == nil {
		return nil // 已存在,跳过
	}

	hash, err := crypto.HashPassword(ba.Password)
	if err != nil {
		return err
	}
	if err := st.CreateUser(ctx, &model.User{
		ID: uuid.NewString(), TenantID: model.PlatformTenantID, Email: ba.Email, PasswordHash: hash,
		Role: model.RolePlatformAdmin, Status: "active", BalanceCents: 0, CreatedAt: time.Now(),
	}); err != nil {
		return err
	}
	log.Printf("[bootstrap] created platform admin: %s", ba.Email)
	return nil
}
