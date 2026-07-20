package main

import (
	"context"
	"log"
	"time"

	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/google/uuid"
)

// seed 灌入开发/演示用的 mock 数据。幂等: 已存在则跳过。
// cipher 用于给真实供应商渠道加密占位密钥(合法密文,需替换为真实 key 方能调用上游)。
func seed(st *store.Store, cipher *crypto.Cipher) error {
	ctx := context.Background()
	now := time.Now()

	// 平台内置租户: 承载平台超级管理员。
	if _, err := st.GetTenant(ctx, model.PlatformTenantID); err != nil {
		if err := st.CreateTenant(ctx, &model.Tenant{
			ID: model.PlatformTenantID, Name: "平台管理", Slug: "platform", Status: "active", CreatedAt: now,
		}); err != nil {
			return err
		}
		log.Println("created platform tenant")
	}

	tenantID := "tenant-demo"
	exists := true
	if _, err := st.GetTenant(ctx, tenantID); err != nil {
		exists = false
	}
	if !exists {
		if err := st.CreateTenant(ctx, &model.Tenant{
			ID: tenantID, Name: "演示租户", Slug: "demo", Status: "active", CreatedAt: now,
		}); err != nil {
			return err
		}
		log.Println("created tenant: tenant-demo")
	}

	// 平台超级管理员(跨租户管理全局资源)。
	adminID := "user-admin"
	if _, err := st.GetUser(ctx, adminID); err != nil {
		hash, _ := crypto.HashPassword("admin123")
		if err := st.CreateUser(ctx, &model.User{
			ID: adminID, TenantID: model.PlatformTenantID, Email: "admin@demo.com", PasswordHash: hash,
			Role: model.RolePlatformAdmin, Status: "active", BalanceCents: 0, CreatedAt: now,
		}); err != nil {
			return err
		}
		log.Println("created platform admin: admin@demo.com / admin123")
	}

	// demo 用户(有余额)
	demoID := "user-demo"
	if _, err := st.GetUser(ctx, demoID); err != nil {
		hash, _ := crypto.HashPassword("demo123")
		if err := st.CreateUser(ctx, &model.User{
			ID: demoID, TenantID: tenantID, Email: "demo@demo.com", PasswordHash: hash,
			Role: model.RoleMember, Status: "active", BalanceCents: 5_000_00, CreatedAt: now,
		}); err != nil {
			return err
		}
		log.Println("created user: demo@demo.com / demo123")
	}

	// 演示模型(售价=分/百万token)。定价采集自各供应商官方价格页(2026-07),均为当前一代主力、
	// 非下线模型。单位换算: 元/百万token × 100 = 分/百万token。成本由挂载渠道(供应商)决定,不在模型层配置。
	models := []*model.ModelDef{
		// 阿里云百炼 — 通义千问当前一代
		{ModelName: "qwen3.7-max", InputPriceCentsPerM: 1200, OutputPriceCentsPerM: 3600, Enabled: true,
			Description: "通义千问最新全能旗舰(2026-05)", Capabilities: []string{"text", "vision"}, ContextLength: 1000000,
			Tags: []string{"视觉", "推理", "长上下文", "最新"}, Providers: []string{"bailian"},
			LongDesc: "阿里云通义千问 Qwen3.7-Max,支持思考/非思考双模式与超长上下文,复杂指令与多模态能力强。"},
		{ModelName: "qwen3-max", InputPriceCentsPerM: 240, OutputPriceCentsPerM: 960, Enabled: true,
			Description: "通义千问主力旗舰", Capabilities: []string{"text"}, ContextLength: 262144,
			Tags: []string{"中文", "推理", "工具"}, Providers: []string{"bailian"},
			LongDesc: "通义千问主力旗舰模型,复杂指令理解与工具调用能力均衡。"},
		{ModelName: "qwen-plus", InputPriceCentsPerM: 80, OutputPriceCentsPerM: 200, Enabled: true,
			Description: "通义千问均衡模型", Capabilities: []string{"text", "vision"}, ContextLength: 131072,
			Tags: []string{"均衡", "视觉", "高性价比"}, Providers: []string{"bailian"},
			LongDesc: "效果与成本均衡,适合中等复杂度任务,支持多模态输入。"},
		{ModelName: "qwen-turbo", InputPriceCentsPerM: 30, OutputPriceCentsPerM: 60, Enabled: true,
			Description: "通义千问极速轻量", Capabilities: []string{"text"}, ContextLength: 1000000,
			Tags: []string{"极速", "低成本", "长上下文"}, Providers: []string{"bailian"},
			LongDesc: "极速响应、极低成本,支持百万级上下文,适合大规模日常调用。"},
		{ModelName: "qwq-plus", InputPriceCentsPerM: 160, OutputPriceCentsPerM: 400, Enabled: true,
			Description: "通义千问深度推理", Capabilities: []string{"text"}, ContextLength: 131072,
			Tags: []string{"推理", "数学", "代码"}, Providers: []string{"bailian"},
			LongDesc: "专用深度思考推理模型,数学、逻辑与代码推理表现突出。"},
		// 火山方舟 — 豆包大模型 1.6(当前一代,2025-06)
		{ModelName: "doubao-seed-1.6", InputPriceCentsPerM: 80, OutputPriceCentsPerM: 800, Enabled: true,
			Description: "豆包 1.6 All-in-One 旗舰", Capabilities: []string{"text", "vision"}, ContextLength: 256000,
			Tags: []string{"思考", "视觉", "256K"}, Providers: []string{"volcark"},
			LongDesc: "字节豆包大模型 1.6 综合模型,支持深度思考、多模态理解与 256K 上下文,自适应思考平衡效果与成本。"},
		{ModelName: "doubao-seed-1.6-flash", InputPriceCentsPerM: 80, OutputPriceCentsPerM: 800, Enabled: true,
			Description: "豆包 1.6 极速版", Capabilities: []string{"text", "vision"}, ContextLength: 256000,
			Tags: []string{"极速", "视觉", "低延迟"}, Providers: []string{"volcark"},
			LongDesc: "豆包 1.6 极速版本,首 token 延迟约 10ms,视觉理解比肩旗舰,适合实时交互场景。"},
		// 百度千帆 — 文心 ERNIE 当前一代
		{ModelName: "ERNIE-5.1", InputPriceCentsPerM: 400, OutputPriceCentsPerM: 1800, Enabled: true,
			Description: "文心 ERNIE 5.1 最新旗舰", Capabilities: []string{"text"}, ContextLength: 128000,
			Tags: []string{"旗舰", "中文", "最新"}, Providers: []string{"qianfan"},
			LongDesc: "百度文心大模型 ERNIE 5.1,最新一代旗舰,中文理解与综合能力领先。"},
		{ModelName: "ERNIE-4.5-Turbo-128K", InputPriceCentsPerM: 80, OutputPriceCentsPerM: 320, Enabled: true,
			Description: "文心 ERNIE 4.5 Turbo 高性价比", Capabilities: []string{"text"}, ContextLength: 128000,
			Tags: []string{"高性价比", "长上下文", "Turbo"}, Providers: []string{"qianfan"},
			LongDesc: "文心 ERNIE 4.5 Turbo,128K 长上下文,高性价比主力模型,支持上下文缓存。"},
		// DeepSeek — V4 当前一代(2026;旧 deepseek-chat/reasoner 2026-07-24 弃用,故用 V4)
		{ModelName: "deepseek-v4-flash", InputPriceCentsPerM: 100, OutputPriceCentsPerM: 200, CacheReadPriceCentsPerM: 2, Enabled: true,
			Description: "DeepSeek V4 极速版(默认非思考/可思考)", Capabilities: []string{"text"}, ContextLength: 1000000,
			Tags: []string{"极速", "长上下文", "硬盘缓存"}, Providers: []string{"deepseek"},
			LongDesc: "DeepSeek-V4-Flash,1M 超长上下文,首创硬盘缓存(命中低至 0.02 元/M),默认非思考可切换思考模式。"},
		{ModelName: "deepseek-v4-pro", InputPriceCentsPerM: 300, OutputPriceCentsPerM: 600, CacheReadPriceCentsPerM: 3, Enabled: true,
			Description: "DeepSeek V4 Pro 深度推理旗舰", Capabilities: []string{"text"}, ContextLength: 1000000,
			Tags: []string{"推理", "旗舰", "长上下文"}, Providers: []string{"deepseek"},
			LongDesc: "DeepSeek-V4-Pro,深度推理旗舰,1M 上下文,复杂推理与代码能力突出。"},
		// 智谱 GLM — 当前一代旗舰(GLM-5.2 最新 / GLM-4.7 高性价比)
		{ModelName: "glm-5.2", InputPriceCentsPerM: 800, OutputPriceCentsPerM: 2800, CacheReadPriceCentsPerM: 200, Enabled: true,
			Description: "智谱 GLM-5.2 最新旗舰(2026-06)", Capabilities: []string{"text"}, ContextLength: 1000000,
			Tags: []string{"旗舰", "最新", "长程任务", "Coding"}, Providers: []string{"zhipuai"},
			LongDesc: "智谱 GLM-5.2,百万 token 稳定上下文,面向 Agentic Coding 与长程任务强化,MIT 协议开源。"},
		{ModelName: "glm-4.7", InputPriceCentsPerM: 200, OutputPriceCentsPerM: 800, CacheReadPriceCentsPerM: 40, Enabled: true,
			Description: "智谱 GLM-4.7 高性价比主力", Capabilities: []string{"text"}, ContextLength: 200000,
			Tags: []string{"高性价比", "通用", "开源"}, Providers: []string{"zhipuai"},
			LongDesc: "智谱 GLM-4.7,200K 上下文,质量与成本平衡,适合日常对话与内容生成,开放权重可私有部署。"},
	}
	for _, m := range models {
		if err := st.UpsertModel(ctx, m); err != nil {
			return err
		}
	}
	log.Printf("upserted %d models", len(models))

	// 平台默认渠道: 1 个 mock 兜底(覆盖全部模型,demo 直连可用) + 5 个真实供应商渠道
	// (各只承载自家模型,填真实供应商成本;占位密钥需替换为真实 key 方能调用上游)。
	// 真实渠道 priority 低于 mock → demo 默认命中 mock 秒回;真实渠道作为故障转移候选 +
	// 在管理端呈现"一渠道一供应商、每模型不同成本"的真实形态。
	all := []string{"qwen3.7-max", "qwen3-max", "qwen-plus", "qwen-turbo", "qwq-plus",
		"doubao-seed-1.6", "doubao-seed-1.6-flash", "ERNIE-5.1", "ERNIE-4.5-Turbo-128K",
		"deepseek-v4-flash", "deepseek-v4-pro", "glm-5.2", "glm-4.7"}
	placeholderKey := ""
	if cipher != nil {
		placeholderKey, _ = cipher.Encrypt("replace-with-real-api-key")
	}

	// 真实供应商渠道定义: provider 统一为 openaicomp(adapter 类型),具体供应商由 baseURL 区分。
	// base_url 与前端 web/admin/src/format.js 的 PROVIDER_TEMPLATES 同源,改动需同步。
	// costs 值为 [4]int64{输入, 输出, 缓存读, 缓存写}。
	type chSeed struct {
		baseURL, name string
		models        []string
		defaultIn     int64
		defaultOut    int64
		costs         map[string][4]int64
	}
	realChs := []chSeed{
		{baseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", name: "百炼 · 通义千问",
			models: []string{"qwen3.7-max", "qwen3-max", "qwen-plus", "qwen-turbo", "qwq-plus"},
			defaultIn: 168, defaultOut: 672,
			costs: map[string][4]int64{
				"qwen3.7-max": {840, 2520, 0, 0},
				"qwen3-max":   {168, 672, 0, 0},
				"qwen-plus":   {56, 140, 0, 0},
				"qwen-turbo":  {21, 42, 0, 0},
				"qwq-plus":    {112, 280, 0, 0},
			}},
		{baseURL: "https://ark.cn-beijing.volces.com/api/v3", name: "火山方舟 · 豆包",
			models: []string{"doubao-seed-1.6", "doubao-seed-1.6-flash"},
			defaultIn: 56, defaultOut: 560,
			costs: map[string][4]int64{
				"doubao-seed-1.6":       {56, 560, 0, 0},
				"doubao-seed-1.6-flash": {48, 160, 0, 0},
			}},
		{baseURL: "https://qianfan.baidubce.com/v2", name: "千帆 · 文心",
			models: []string{"ERNIE-5.1", "ERNIE-4.5-Turbo-128K"},
			defaultIn: 280, defaultOut: 1260,
			costs: map[string][4]int64{
				"ERNIE-5.1":            {280, 1260, 0, 0},
				"ERNIE-4.5-Turbo-128K": {56, 224, 0, 0},
			}},
		{baseURL: "https://api.deepseek.com", name: "DeepSeek",
			models: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			defaultIn: 70, defaultOut: 140,
			costs: map[string][4]int64{
				"deepseek-v4-flash": {70, 140, 1, 0},
				"deepseek-v4-pro":   {210, 420, 3, 0},
			}},
		{baseURL: "https://open.bigmodel.cn/api/paas/v4", name: "智谱 · GLM",
			models: []string{"glm-5.2", "glm-4.7"},
			defaultIn: 140, defaultOut: 560,
			costs: map[string][4]int64{
				"glm-5.2": {560, 1960, 80, 0},
				"glm-4.7": {140, 560, 28, 0},
			}},
	}

	// buildChannelModels 由模型清单 + 成本表构造 []ChannelModel(每个渠道×模型一行)。
	buildChannelModels := func(models []string, costs map[string][4]int64) []model.ChannelModel {
		out := make([]model.ChannelModel, 0, len(models))
		for _, m := range models {
			c := costs[m] // 缺省 [4]int64{0,0,0,0} → 回退渠道级默认成本
			out = append(out, model.ChannelModel{
				ID: uuid.NewString(), ModelName: m,
				InputCostCentsPerM: c[0], OutputCostCentsPerM: c[1],
				CacheReadCostCentsPerM: c[2], CacheWriteCostCentsPerM: c[3],
				Weight: 1, Status: "active", CreatedAt: now,
			})
		}
		return out
	}

	channels, _ := st.ListChannels(ctx, "")
	// provider 统一为 openaicomp 后无法按 provider 区分供应商,改按渠道名(name)去重。
	existNames := map[string]bool{}
	hasMock := false
	for _, c := range channels {
		if c.TenantID != nil {
			continue
		}
		if c.Provider == "mock" {
			hasMock = true
		} else {
			existNames[c.Name] = true
		}
	}
	if !hasMock {
		if err := st.CreateChannel(ctx, &model.Channel{
			ID: uuid.NewString(), Provider: "mock", Name: "Mock 兜底渠道",
			BaseURL: "", APIKeyEnc: placeholderKey, ChannelModels: buildChannelModels(all, nil),
			Priority: 10, Weight: 1, Status: "active", CreatedAt: now,
		}); err != nil {
			return err
		}
		log.Println("created mock fallback channel (priority=10)")
	}
	for _, rc := range realChs {
		if existNames[rc.name] {
			continue
		}
		if err := st.CreateChannel(ctx, &model.Channel{
			ID: uuid.NewString(), Provider: "openaicomp", Name: rc.name,
			BaseURL: rc.baseURL, APIKeyEnc: placeholderKey, ChannelModels: buildChannelModels(rc.models, rc.costs),
			InputCostCentsPerM: rc.defaultIn, OutputCostCentsPerM: rc.defaultOut,
			Priority: 1, Weight: 1, Status: "active", CreatedAt: now,
		}); err != nil {
			return err
		}
		log.Printf("created channel: %s (%s) with %d model-level costs", rc.name, rc.baseURL, len(rc.costs))
	}

	// 给 demo 用户一张 API Key(便于命令行测试)
	demoKeyHash := crypto.APIKeyHash("sk-demo-key-1234567890")
	if _, err := st.GetAPIKeyByHash(ctx, demoKeyHash); err != nil {
		if err := st.CreateAPIKey(ctx, &model.APIKey{
			ID: uuid.NewString(), TenantID: tenantID, UserID: demoID,
			KeyPrefix: "sk-demo-k", KeyHash: demoKeyHash, Name: "demo-key",
			Scopes: []string{"chat"}, Models: []string{}, Status: "active", CreatedAt: now,
		}); err != nil {
			return err
		}
		log.Println("created demo api key: sk-demo-key-1234567890")
	}
	return nil
}
