// Package model 定义持久化实体。
package model

import "time"

// Tenant 租户。
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"` // active | disabled
	CreatedAt time.Time `json:"created_at"`
}

// Role 用户角色。
type Role string

const (
	// RolePlatformAdmin 平台超级管理员: 跨租户管理全局资源(租户/全局模型定价/渠道/账目/审计)。
	// 由配置 bootstrap_admin 或 seed 创建,不可自助注册获得。
	RolePlatformAdmin Role = "platform_admin"
	// RoleAdmin 租户管理员: 仅对本租户的资源有管理权(本租户用户/渠道/用量)。
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// PlatformTenantID 平台内置租户: 承载 platform_admin 用户与平台默认渠道。
// 不在前端租户列表暴露为业务租户,仅作 platform_admin 的归属。
const PlatformTenantID = "tenant-platform"

// User 用户。
type User struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // 永不序列化输出,避免哈希泄露
	Role         Role      `json:"role"`
	Status       string    `json:"status"` // active | disabled
	BalanceCents int64     `json:"balance_cents"`
	CreatedAt    time.Time `json:"created_at"`
}

// APIKey 用户/租户的接口密钥。
type APIKey struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	UserID      string     `json:"user_id"`
	KeyPrefix   string     `json:"key_prefix"`
	KeyHash     string     `json:"-"`
	Name        string     `json:"name"`
	Scopes      []string   `json:"scopes"`
	Models      []string   `json:"models"`
	RPMLimit    int        `json:"rpm_limit"`
	TPMLimit    int        `json:"tpm_limit"`
	// 日/月用量配额(0=不限):请求数与 token 上限,配合 RPM/TPM 形成"分钟→天→月"递进限流。
	DailyRequestLimit   int `json:"daily_request_limit"`
	MonthlyRequestLimit int `json:"monthly_request_limit"`
	DailyTokenLimit     int `json:"daily_token_limit"`
	MonthlyTokenLimit   int `json:"monthly_token_limit"`
	IPWhitelist []string   `json:"ip_whitelist"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	Status      string     `json:"status"` // active | revoked
	CreatedAt   time.Time  `json:"created_at"`
}

// Channel 渠道(tenant_id 为空表示平台默认)。
// 一个逻辑模型可由多个渠道承载(多家供应商),实现负载均衡与故障转移。
// 渠道级属性: provider/密钥/优先级/权重/渠道级默认成本。
// 「该渠道支持哪些模型 + 每个模型的上游名/成本/权重/启停」规范化到 ChannelModels(独立实体)。
type Channel struct {
	ID       string  `json:"id"`
	TenantID *string `json:"tenant_id"`
	Provider string  `json:"provider"`
	Name     string  `json:"name"`
	BaseURL  string  `json:"base_url"`
	APIKeyEnc string `json:"-"` // AES-GCM 密文,永不序列化
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	Status   string `json:"status"` // active | disabled
	// 渠道级默认成本: 当某模型的 ChannelModel 成本为 0 时回退到此(减少重复填写)。
	InputCostCentsPerM  int64 `json:"input_cost_cents_per_m"`
	OutputCostCentsPerM int64 `json:"output_cost_cents_per_m"`
	// 模型清单(列表/编辑时联表带回;route 不用此字段,改用 ChannelsForModel 的 JOIN 单行结果)。
	ChannelModels []ChannelModel `json:"channel_models,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// ChannelModel 一个「渠道 × 模型」的独立配置(取代内嵌的 models[]/model_mappings/model_costs)。
// 每行可独立配上游名、成本、权重、启停——单模型可禁用而不影响同渠道其他模型。
type ChannelModel struct {
	ID                      string    `json:"id"`
	ChannelID               string    `json:"channel_id"`
	ModelName               string    `json:"model_name"`         // 逻辑模型名(对外)
	UpstreamModel           string    `json:"upstream_model"`     // 空=同名直通
	InputCostCentsPerM      int64     `json:"input_cost_cents_per_m"`
	OutputCostCentsPerM     int64     `json:"output_cost_cents_per_m"`
	CacheReadCostCentsPerM  int64     `json:"cache_read_cost_cents_per_m"`
	CacheWriteCostCentsPerM int64     `json:"cache_write_cost_cents_per_m"`
	Weight                  int       `json:"weight"`  // 0=继承渠道 weight
	Status                  string    `json:"status"`  // active | disabled
	CreatedAt               time.Time `json:"created_at"`
}

// ModelNames 返回该渠道所有模型的逻辑名(供列表展示/测试取首个模型)。
func (c *Channel) ModelNames() []string {
	out := make([]string, 0, len(c.ChannelModels))
	for _, cm := range c.ChannelModels {
		out = append(out, cm.ModelName)
	}
	return out
}

// ModelDef 模型与全局定价(售价 + 成本价) + 展示元数据。
type ModelDef struct {
	ModelName                string   `json:"model_name"`
	InputPriceCentsPerM      int64    `json:"input_price_cents_per_m"`
	OutputPriceCentsPerM     int64    `json:"output_price_cents_per_m"`
	CacheReadPriceCentsPerM  int64    `json:"cache_read_price_cents_per_m"`
	CacheWritePriceCentsPerM int64    `json:"cache_write_price_cents_per_m"`
	Enabled                  bool     `json:"enabled"`
	Description              string   `json:"description"`
	LongDesc                 string   `json:"long_desc"`
	Tags                     []string `json:"tags"`
	Capabilities             []string `json:"capabilities"`
	ContextLength            int      `json:"context_length"`
	RoutingStrategy          string   `json:"routing_strategy"`
	PinnedChannelID          string   `json:"pinned_channel_id"`
	Providers                []string `json:"providers,omitempty"`
}

// 支持的路由策略枚举。
const (
	StrategyWeighted   = "weighted"
	StrategyRoundRobin = "round_robin"
	StrategyFailover   = "failover"
	StrategyRandom     = "random"
	StrategyPinned     = "pinned"
)

// NormalizedRoutingStrategy 归一化策略值,非法/空值回退 weighted。
func NormalizedRoutingStrategy(s string) string {
	switch s {
	case StrategyRoundRobin, StrategyFailover, StrategyRandom, StrategyPinned:
		return s
	default:
		return StrategyWeighted
	}
}

// TenantModelOverride 租户级模型定价覆盖。
type TenantModelOverride struct {
	TenantID             string `json:"tenant_id"`
	ModelName            string `json:"model_name"`
	InputPriceCentsPerM  int64  `json:"input_price_cents_per_m"`
	OutputPriceCentsPerM int64  `json:"output_price_cents_per_m"`
	Enabled              bool   `json:"enabled"`
}

// LedgerType 账目类型。
type LedgerType string

const (
	LedgerRecharge LedgerType = "recharge"
	LedgerUsage    LedgerType = "usage"
	LedgerRefund   LedgerType = "refund"
	LedgerTransfer LedgerType = "transfer" // 团队内团长→成员的余额转账(出账/入账成对)
	LedgerAdjust   LedgerType = "adjust"   // 管理员手动调整余额(加/扣),走账本保账实一致
)

// BillingLedger 计费账目。
type BillingLedger struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	UserID       string     `json:"user_id"`
	RequestID    string     `json:"request_id"`
	Model        string     `json:"model"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	CostCents    int64      `json:"cost_cents"`
	PriceCents   int64      `json:"price_cents"`
	MarginCents  int64      `json:"margin_cents"`
	Type         LedgerType `json:"type"`
	BalanceAfter int64      `json:"balance_after"`
	CreatedAt    time.Time  `json:"created_at"`
}

// UsageRecord 用量明细。
type UsageRecord struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	UserID       string    `json:"user_id"`
	APIKeyID     string    `json:"api_key_id"`
	APIKeyName   string    `json:"api_key_name"`
	RequestID    string    `json:"request_id"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	ChannelID    string    `json:"channel_id"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	LatencyMs    int       `json:"latency_ms"`
	PriceCents   int64     `json:"price_cents"`
	CostCents    int64     `json:"cost_cents"`
	Status       string    `json:"status"`        // ok | error
	ErrorMessage string    `json:"error_message"` // status=error 时的脱敏错误摘要
	CreatedAt    time.Time `json:"created_at"`
}

// Recharge 充值记录。
type Recharge struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	UserID      string    `json:"user_id"`
	AmountCents int64     `json:"amount_cents"`
	Status      string    `json:"status"` // pending | success | failed
	CreatedAt   time.Time `json:"created_at"`
}

// 支付订单状态。
const (
	PaymentStatusPending = "pending"
	PaymentStatusPaid    = "paid"
	PaymentStatusClosed  = "closed"
)

// 计费重试队列状态。
const (
	PendingChargePending   = "pending"
	PendingChargeDone      = "done"
	PendingChargeAbandoned = "abandoned"
)

// InviteToken 团队邀请令牌。明文 token 仅创建时返回一次;DB 存 hash,支持列表/吊销/审计。
type InviteToken struct {
	ID        string     `json:"id"`
	TokenHash string     `json:"-"`
	TenantID  string     `json:"tenant_id"`
	Role      string     `json:"role"` // member | admin
	CreatedBy string     `json:"created_by"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
	UsedBy    *string    `json:"used_by"`
	CreatedAt time.Time  `json:"created_at"`
}

// PendingCharge 计费失败重试项。// relay 在响应完成后计费,若失败则把应扣项落库,由后台 worker 幂等重试,防漏账。
// request_id 为幂等键(usage 请求级唯一,配合 billing_ledger 的部分唯一索引)。
type PendingCharge struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	UserID           string    `json:"user_id"`
	RequestID        string    `json:"request_id"`
	Model            string    `json:"model"`
	InputTokens      int       `json:"input_tokens"`
	OutputTokens     int       `json:"output_tokens"`
	CacheReadTokens  int       `json:"cache_read_tokens"`
	CacheWriteTokens int       `json:"cache_write_tokens"`
	PriceCents       int64     `json:"price_cents"`
	CostCents        int64     `json:"cost_cents"`
	Attempts         int       `json:"attempts"`
	Status           string    `json:"status"`
	LastError        *string   `json:"last_error"`
	NextRetryAt      time.Time `json:"next_retry_at"`
	CreatedAt        time.Time `json:"created_at"`
}

// 支付渠道标识(支付宝用于回调 ACK 文案区分;微信/mock 由各 provider 自行 Name())。
const (
	ProviderAlipay = "alipay"
)

// PaymentOrder 支付订单(承接第三方支付下单/回调的全生命周期)。
//   - out_trade_no 为商户订单号,全局唯一,作为幂等键抵御回调重入。
//   - status 状态机: pending → paid(已入账) / closed(超时关单)。
//   - 回调成功后由 billing.Recharge 入账;余额变更只发生一次(MarkPaid 仅 pending→paid 返回 true)。
type PaymentOrder struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	UserID        string     `json:"user_id"`
	OutTradeNo    string     `json:"out_trade_no"`
	Provider      string     `json:"provider"`
	AmountCents   int64      `json:"amount_cents"`
	Status        string     `json:"status"`
	PrepayData    *string    `json:"prepay_data"`
	TransactionID *string    `json:"transaction_id"`
	PaidAt        *time.Time `json:"paid_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// File 上传文件元信息。
type File struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	UserID     string    `json:"user_id"`
	Filename   string    `json:"filename"`
	MimeType   string    `json:"mime_type"`
	Size       int64     `json:"size"`
	StorageURL string    `json:"storage_url"`
	Purpose    string    `json:"purpose"`
	CreatedAt  time.Time `json:"created_at"`
}

// AuditLog 审计日志。
type AuditLog struct {
	ID        string    `json:"id"`
	ActorID   string    `json:"actor_id"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Payload   []byte    `json:"payload,omitempty"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"created_at"`
}

// RequestLog 请求/响应原文日志(生产排障/合规审计)。
// request_body/response_body/error 可空(采样或 LogBodies=false),故用指针 nullable 扫描。
type RequestLog struct {
	ID           string     `json:"id"`
	RequestID    string     `json:"request_id"`
	TenantID     string     `json:"tenant_id"`
	UserID       string     `json:"user_id"`
	APIKeyID     string     `json:"api_key_id"`
	Model        string     `json:"model"`
	Provider     string     `json:"provider"`
	ChannelID    string     `json:"channel_id"`
	Method       string     `json:"method"`
	Path         string     `json:"path"`
	Status       int        `json:"status"`
	LatencyMs    int        `json:"latency_ms"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	PriceCents   int64      `json:"price_cents"`
	RequestBody  *string    `json:"request_body,omitempty"`
	ResponseBody *string    `json:"response_body,omitempty"`
	Error        *string    `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}
