package store

import (
	"context"
	"testing"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
)

// TestChannelModelsCRUD 覆盖 channel_models 独立实体的核心生命周期:
// 创建渠道(带模型) → JOIN 查询(ChannelsForModel) → 单模型启停 → 单模型增删 → 删除渠道级联。
func TestChannelModelsCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-cm-" + storeID()
	uid := seedUser(t, s, tenant, 0)
	_ = uid

	now := time.Now()
	ch := &model.Channel{
		ID: storeID(), Provider: "mock", Name: "cm-test",
		Status: "active", Priority: 1, Weight: 1, CreatedAt: now,
		ChannelModels: []model.ChannelModel{
			{ID: storeID(), ModelName: "m-alpha", InputCostCentsPerM: 100, OutputCostCentsPerM: 200, Weight: 1, Status: "active", CreatedAt: now},
			{ID: storeID(), ModelName: "m-beta", UpstreamModel: "beta-real", InputCostCentsPerM: 50, OutputCostCentsPerM: 80, CacheReadCostCentsPerM: 5, Weight: 1, Status: "active", CreatedAt: now},
		},
	}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// ChannelsForModel 应 JOIN 返回渠道 + 该模型配置(ChannelModels 仅含该模型一项)。
	got, err := s.ChannelsForModel(ctx, tenant, "m-beta")
	if err != nil {
		t.Fatalf("channels for model: %v", err)
	}
	if len(got) != 1 || len(got[0].ChannelModels) != 1 {
		t.Fatalf("want 1 channel with 1 model cfg got %+v", got)
	}
	cm := got[0].ChannelModels[0]
	if cm.UpstreamModel != "beta-real" || cm.OutputCostCentsPerM != 80 || cm.CacheReadCostCentsPerM != 5 {
		t.Fatalf("model cfg mismatch: %+v", cm)
	}

	// 单模型启停: 禁用 m-alpha 后,ChannelsForModel(active) 应不再返回它。
	if err := s.SetChannelModelStatus(ctx, ch.ID, "m-alpha", "disabled"); err != nil {
		t.Fatalf("disable model: %v", err)
	}
	if got, _ := s.ChannelsForModel(ctx, tenant, "m-alpha"); len(got) != 0 {
		t.Fatalf("disabled model should not be returned, got %d", len(got))
	}
	// 同渠道 m-beta 不受影响。
	if got, _ := s.ChannelsForModel(ctx, tenant, "m-beta"); len(got) != 1 {
		t.Fatalf("m-beta should still be active, got %d", len(got))
	}

	// 单模型增删: 挂载 m-gamma,再移除。
	if err := s.AddChannelModel(ctx, ch.ID, "m-gamma"); err != nil {
		t.Fatalf("add model: %v", err)
	}
	if got, _ := s.ChannelsForModel(ctx, tenant, "m-gamma"); len(got) != 1 {
		t.Fatalf("m-gamma should be mounted, got %d", len(got))
	}
	if err := s.DeleteChannelModel(ctx, ch.ID, "m-gamma"); err != nil {
		t.Fatalf("remove model: %v", err)
	}
	if got, _ := s.ChannelsForModel(ctx, tenant, "m-gamma"); len(got) != 0 {
		t.Fatalf("m-gamma should be removed, got %d", len(got))
	}

	// 列表带出 channel_models(ListChannels 批量联表)。
	chs, err := s.ListChannels(ctx, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, c := range chs {
		if c.ID == ch.ID {
			// m-alpha disabled + m-beta active = 2 行(ListChannelModels 不过滤 status)。
			if len(c.ChannelModels) != 2 {
				t.Fatalf("want 2 channel_models got %d", len(c.ChannelModels))
			}
			found = true
		}
	}
	if !found {
		t.Fatal("created channel not in list")
	}

	// 删除渠道级联清 channel_models。
	if err := s.DeleteChannel(ctx, ch.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	cms, _ := s.ListChannelModels(ctx, ch.ID)
	if len(cms) != 0 {
		t.Fatalf("cascade delete should clear channel_models, got %d", len(cms))
	}
}

// TestUpdateChannelReplacesModels 验证 UpdateChannel 全量替换模型清单(删旧插新)。
func TestUpdateChannelReplacesModels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	ch := &model.Channel{
		ID: storeID(), Provider: "mock", Name: "replace-test", Status: "active", Weight: 1, CreatedAt: now,
		ChannelModels: []model.ChannelModel{
			{ID: storeID(), ModelName: "old-1", Weight: 1, Status: "active", CreatedAt: now},
			{ID: storeID(), ModelName: "old-2", Weight: 1, Status: "active", CreatedAt: now},
		},
	}
	if err := s.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	// 全量替换为新的两个模型(旧的两个应消失)。
	ch.ChannelModels = []model.ChannelModel{
		{ID: storeID(), ModelName: "new-1", Weight: 1, Status: "active", CreatedAt: now},
		{ID: storeID(), ModelName: "new-2", Weight: 1, Status: "active", CreatedAt: now},
		{ID: storeID(), ModelName: "new-3", Weight: 1, Status: "active", CreatedAt: now},
	}
	if err := s.UpdateChannel(ctx, ch); err != nil {
		t.Fatalf("update: %v", err)
	}
	cms, _ := s.ListChannelModels(ctx, ch.ID)
	if len(cms) != 3 {
		t.Fatalf("want 3 models after replace got %d", len(cms))
	}
	for _, cm := range cms {
		if cm.ModelName == "old-1" || cm.ModelName == "old-2" {
			t.Fatalf("old model should be removed: %s", cm.ModelName)
		}
	}
}
