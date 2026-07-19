// Package config 负责加载配置。来源优先级: 环境变量 > config.local.yaml > config.yaml > 默认值。
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 顶层配置。
type Config struct {
	Dev      bool     `yaml:"dev"` // 开发模式: 放宽密钥校验、放开模拟充值/注册。生产必须为 false。
	Log      Log      `yaml:"log"`
	Server   Server   `yaml:"server"`
	Postgres Postgres `yaml:"postgres"`
	Redis    Redis    `yaml:"redis"`
	Auth     Auth     `yaml:"auth"`
	Files    Files    `yaml:"files"`
	Billing  Billing  `yaml:"billing"`
	ReqLog   ReqLog   `yaml:"req_log"`
	Defaults Defaults `yaml:"defaults"`
	Web      Web      `yaml:"web"`
	Edge     Edge     `yaml:"edge"`
	Payment  Payment  `yaml:"payment"`
}

// Log 结构化日志配置。
type Log struct {
	Level  string `yaml:"level"`  // debug | info(默认) | warn | error
	Format string `yaml:"format"` // json | text(默认)
}

type Server struct {
	Addr            string   `yaml:"addr"`
	PublicURL       string   `yaml:"public_url"`
	CORSOrigins     []string `yaml:"cors_origins"`     // 允许的跨域 Origin;为空则不发送 CORS 头(仅同源)
	TrustedProxies  []string `yaml:"trusted_proxies"`  // 受信代理 CIDR(用于 ClientIP 解析 X-Forwarded-For);空=不信任任何代理
}

type Postgres struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"sslmode"`
	MaxConns int    `yaml:"max_conns"`
}

// DSN 拼装 Postgres 连接串。
func (p Postgres) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		p.Host, p.Port, p.User, p.Password, p.Database, p.SSLMode)
}

type Redis struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type Auth struct {
	JWTSecret        string         `yaml:"jwt_secret"`
	AccessTTL        string         `yaml:"access_ttl"`
	RefreshTTL       string         `yaml:"refresh_ttl"`
	ChannelKeyMaster string         `yaml:"channel_key_master"`
	AllowSignup      bool           `yaml:"allow_signup"`    // 是否放开 /api/auth/register;生产建议 false
	BootstrapAdmin   BootstrapAdmin `yaml:"bootstrap_admin"` // 启动时确保存在的平台超级管理员
}

// BootstrapAdmin 平台超级管理员引导配置。
// 启动时若该 email 不存在则创建为 platform_admin;已存在则跳过。
// 用于首次部署获得跨租户管理权(自助注册只能得到租户级 admin)。
type BootstrapAdmin struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}

type Files struct {
	Storage   string `yaml:"storage"`
	LocalRoot string `yaml:"local_root"`
	BaseURL   string `yaml:"base_url"`
}

type Billing struct {
	MinBalanceCents int64 `yaml:"min_balance_cents"`
	CharsPerToken   int   `yaml:"chars_per_token"`
}

// ReqLog 请求/响应原文日志配置。默认关闭(隐私与存储考量),按需开启。
type ReqLog struct {
	Enabled      bool    `yaml:"enabled"`        // 总开关
	SampleRate   float64 `yaml:"sample_rate"`    // 采样率 0~1,1=全量
	MaxBodyBytes int     `yaml:"max_body_bytes"` // 请求/响应体截断长度(字节)
	RetainDays   int     `yaml:"retain_days"`    // 保留天数,超期由 worker 清理
	LogBodies    bool    `yaml:"log_bodies"`     // false=只记元信息不记原文(合规统计场景)
}

type Defaults struct {
	DefaultProvider string `yaml:"default_provider"`
}

// Web 内置前端 SPA 的构建产物路径(空则不托管,前端走独立 dev server)。
type Web struct {
	UserDist  string `yaml:"user_dist"`
	AdminDist string `yaml:"admin_dist"`
}

// Payment 支付配置(预付充值: 微信支付 Native + 支付宝电脑网站支付 + mock)。
//   - base_url: 站点对外根 URL,用于拼异步回调地址(生产必须为公网可达 https)。
//   - mock=true 时仅启用 mock provider,不依赖任何商户资质(开发/测试用)。
//   - 各渠道 enabled 控制是否注册;未配置密钥的渠道不应启用(装配时会跳过并告警)。
type Payment struct {
	BaseURL      string        `yaml:"base_url"`
	ExpiresMin   int           `yaml:"expires_minutes"` // 订单有效期,默认 15
	Mock         bool          `yaml:"mock"`             // 强制只用 mock(便于无商户号验证全链路)
	Wechat       WechatConfig  `yaml:"wechat"`
	Alipay       AlipayConfig  `yaml:"alipay"`
}

// WechatConfig 微信支付(Native 扫码)配置。
type WechatConfig struct {
	Enabled        bool   `yaml:"enabled"`
	AppID          string `yaml:"appid"`
	MchID          string `yaml:"mchid"`
	MchSerialNo    string `yaml:"mch_serial_no"`
	PrivateKey     string `yaml:"private_key"`   // PEM 文本或 apiclient_key.pem 路径
	APIv3Key       string `yaml:"api_v3_key"`
}

// AlipayConfig 支付宝(电脑网站支付)配置。
type AlipayConfig struct {
	Enabled         bool   `yaml:"enabled"`
	AppID           string `yaml:"appid"`
	PrivateKey      string `yaml:"private_key"`      // 应用私钥 PEM 文本
	AlipayPublicKey string `yaml:"alipay_public_key"` // 支付宝公钥 PEM 文本
	Sandbox         bool   `yaml:"sandbox"`           // true=沙箱
}

// Edge 数据面(网关接入点)配置。
//
//	addr 留空: 接入点内嵌于控制面同进程同端口(默认,单实例/开发)。
//	addr 设值(且 != server.addr): 接入点在同进程内独立端口。
//	standalone=true: 控制面(cmd/gateway)完全不内嵌接入点,由独立 cmd/edge 二进制承担(真正的双二进制拆分)。
type Edge struct {
	Addr         string `yaml:"addr"`          // 例 ":8090"; 留空=同端口
	Standalone   bool   `yaml:"standalone"`    // true=使用独立 cmd/edge 二进制
	ReadTimeout  string `yaml:"read_timeout"`  // 默认 0(流式不限制)
	WriteTimeout string `yaml:"write_timeout"` // 默认 0(SSE 不限制)
}

// Load 加载配置。path 可为空(仅环境变量)。
func Load(path string) (*Config, error) {
	c := defaults()
	if path != "" {
		if err := loadYAML(path, c); err != nil {
			return nil, fmt.Errorf("load yaml %s: %w", path, err)
		}
	}
	applyEnv(c)
	return c, nil
}

func defaults() *Config {
	return &Config{
		Server:   Server{Addr: ":8080", PublicURL: "http://localhost:8080"},
		Postgres: Postgres{Host: "localhost", Port: 5432, User: "gateway", Password: "gateway", Database: "gateway", SSLMode: "disable", MaxConns: 20},
		Redis:    Redis{Addr: "localhost:6379"},
		// 注意: JWTSecret / ChannelKeyMaster 不给默认值——生产部署若遗漏配置,
		// Validate() 会拒绝启动,避免用公开弱密钥"裸奔"。开发态设 dev:true 放宽。
		Auth:     Auth{AccessTTL: "15m", RefreshTTL: "168h"},
		Files:    Files{Storage: "local", LocalRoot: "./data/files", BaseURL: "http://localhost:8080"},
		Billing:  Billing{MinBalanceCents: 0, CharsPerToken: 2},
		ReqLog:   ReqLog{Enabled: false, SampleRate: 1.0, MaxBodyBytes: 32 * 1024, RetainDays: 7, LogBodies: true},
		Defaults: Defaults{DefaultProvider: "mock"},
		Web:      Web{UserDist: "web/user/dist", AdminDist: "web/admin/dist"},
		Payment:  Payment{ExpiresMin: 15},
	}
}

// 已知的"示例/弱"密钥,生产配置中出现即视为未配置。
var (
	knownWeakJWTSecrets = map[string]struct{}{
		"": {},
		"change-me-in-production-please-use-32-bytes-or-more": {},
	}
	knownWeakMasters = map[string]struct{}{
		"": {},
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": {},
		"0000000000000000000000000000000000000000000000000000000000000000": {},
	}
)

// Validate 校验生产关键配置。dev 模式下放宽(仅告警),便于本地开发/seed。
func (c *Config) Validate() error {
	if c.Dev {
		// 开发态: 不阻断启动,但确保有值以免空指针。
		if c.Auth.JWTSecret == "" {
			c.Auth.JWTSecret = "dev-insecure-jwt-secret"
		}
		if c.Auth.ChannelKeyMaster == "" || isWeak(c.Auth.ChannelKeyMaster, knownWeakMasters) {
			c.Auth.ChannelKeyMaster = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
		}
		return nil
	}
	if isWeak(c.Auth.JWTSecret, knownWeakJWTSecrets) {
		return fmt.Errorf("auth.jwt_secret 未配置或为示例值;生产环境必须设置一个唯一且足够长的密钥(建议 >= 32 字节)")
	}
	if len(c.Auth.JWTSecret) < 16 {
		return fmt.Errorf("auth.jwt_secret 过短(>= 16 字符);当前 %d 字符", len(c.Auth.JWTSecret))
	}
	if isWeak(c.Auth.ChannelKeyMaster, knownWeakMasters) {
		return fmt.Errorf("auth.channel_key_master 未配置或为示例值;生成: openssl rand -hex 32")
	}
	if _, err := hex.DecodeString(c.Auth.ChannelKeyMaster); err != nil {
		return fmt.Errorf("auth.channel_key_master 不是合法 hex:%w", err)
	}
	// 支付: 启用了某渠道但密钥不全会导致下单/验签失败,生产启动即拦截,避免线上收不了钱。
	// Mock 必须绑定 dev: 生产误开 mock 会让任何人经 /api/payments/mock/notify 无限自充值。
	if c.Payment.Mock && !c.Dev {
		return fmt.Errorf("payment.mock 仅允许在 dev=true 时启用;生产环境会绕过真实支付被白嫖")
	}
	if c.Payment.Wechat.Enabled && !c.Payment.Mock {
		if c.Payment.Wechat.AppID == "" || c.Payment.Wechat.MchID == "" ||
			c.Payment.Wechat.MchSerialNo == "" || c.Payment.Wechat.PrivateKey == "" || c.Payment.Wechat.APIv3Key == "" {
			return fmt.Errorf("payment.wechat 已启用但配置不完整(appid/mchid/mch_serial_no/private_key/api_v3_key)")
		}
	}
	if c.Payment.Alipay.Enabled && !c.Payment.Mock {
		if c.Payment.Alipay.AppID == "" || c.Payment.Alipay.PrivateKey == "" || c.Payment.Alipay.AlipayPublicKey == "" {
			return fmt.Errorf("payment.alipay 已启用但配置不完整(appid/private_key/alipay_public_key)")
		}
	}
	return nil
}

func isWeak(v string, set map[string]struct{}) bool {
	_, ok := set[v]
	return ok
}

func loadYAML(path string, c *Config) error {
	b, err := os.ReadFile(path) //nolint:gosec // path 来自启动 -config 参数,非用户输入
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, c)
}

// applyEnv 以 GATEWAY_ 前缀的环境变量覆盖配置，嵌套用 __ 分隔。
// 例: GATEWAY_POSTGRES__HOST=db  ->  cfg.Postgres.Host = "db"
//
//	GATEWAY_REDIS__DB=1        ->  cfg.Redis.DB = 1
func applyEnv(c *Config) {
	setStr := func(key, dst string) string {
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		return dst
	}
	setBool := func(key string, dst bool) bool {
		if v, ok := os.LookupEnv(key); ok {
			return v == "1" || v == "true" || v == "TRUE"
		}
		return dst
	}
	setInt := func(key string, dst int) int {
		if v, ok := os.LookupEnv(key); ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return dst
	}
	setInt64 := func(key string, dst int64) int64 {
		if v, ok := os.LookupEnv(key); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				return n
			}
		}
		return dst
	}
	setFloat := func(key string, dst float64) float64 {
		if v, ok := os.LookupEnv(key); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
		return dst
	}
	// setStrSlice 逗号分隔;空串视为空切片(覆盖默认)。
	setStrSlice := func(key string, dst []string) []string {
		if v, ok := os.LookupEnv(key); ok {
			if strings.TrimSpace(v) == "" {
				return []string{}
			}
			parts := strings.Split(v, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			return parts
		}
		return dst
	}
	// 手动映射全部字段(环境变量 > yaml > 默认)。命名规则: GATEWAY_<SECTION>__<FIELD>。
	c.Dev = setBool("GATEWAY_DEV", c.Dev)
	c.Log.Level = setStr("GATEWAY_LOG__LEVEL", c.Log.Level)
	c.Log.Format = setStr("GATEWAY_LOG__FORMAT", c.Log.Format)
	c.Server.Addr = setStr("GATEWAY_SERVER__ADDR", c.Server.Addr)
	c.Server.PublicURL = setStr("GATEWAY_SERVER__PUBLIC_URL", c.Server.PublicURL)
	c.Server.CORSOrigins = setStrSlice("GATEWAY_SERVER__CORS_ORIGINS", c.Server.CORSOrigins)
	c.Server.TrustedProxies = setStrSlice("GATEWAY_SERVER__TRUSTED_PROXIES", c.Server.TrustedProxies)
	c.Postgres.Host = setStr("GATEWAY_POSTGRES__HOST", c.Postgres.Host)
	c.Postgres.Password = setStr("GATEWAY_POSTGRES__PASSWORD", c.Postgres.Password)
	c.Postgres.User = setStr("GATEWAY_POSTGRES__USER", c.Postgres.User)
	c.Postgres.Database = setStr("GATEWAY_POSTGRES__DATABASE", c.Postgres.Database)
	c.Postgres.SSLMode = setStr("GATEWAY_POSTGRES__SSLMODE", c.Postgres.SSLMode)
	c.Postgres.Port = setInt("GATEWAY_POSTGRES__PORT", c.Postgres.Port)
	c.Postgres.MaxConns = setInt("GATEWAY_POSTGRES__MAX_CONNS", c.Postgres.MaxConns)
	c.Redis.Addr = setStr("GATEWAY_REDIS__ADDR", c.Redis.Addr)
	c.Redis.Password = setStr("GATEWAY_REDIS__PASSWORD", c.Redis.Password)
	c.Redis.DB = setInt("GATEWAY_REDIS__DB", c.Redis.DB)
	c.Auth.JWTSecret = setStr("GATEWAY_AUTH__JWT_SECRET", c.Auth.JWTSecret)
	c.Auth.ChannelKeyMaster = setStr("GATEWAY_AUTH__CHANNEL_KEY_MASTER", c.Auth.ChannelKeyMaster)
	c.Auth.AccessTTL = setStr("GATEWAY_AUTH__ACCESS_TTL", c.Auth.AccessTTL)
	c.Auth.RefreshTTL = setStr("GATEWAY_AUTH__REFRESH_TTL", c.Auth.RefreshTTL)
	c.Auth.BootstrapAdmin.Email = setStr("GATEWAY_AUTH__BOOTSTRAP_ADMIN__EMAIL", c.Auth.BootstrapAdmin.Email)
	c.Auth.BootstrapAdmin.Password = setStr("GATEWAY_AUTH__BOOTSTRAP_ADMIN__PASSWORD", c.Auth.BootstrapAdmin.Password)
	c.Auth.AllowSignup = setBool("GATEWAY_AUTH__ALLOW_SIGNUP", c.Auth.AllowSignup)
	c.Files.Storage = setStr("GATEWAY_FILES__STORAGE", c.Files.Storage)
	c.Files.LocalRoot = setStr("GATEWAY_FILES__LOCAL_ROOT", c.Files.LocalRoot)
	c.Files.BaseURL = setStr("GATEWAY_FILES__BASE_URL", c.Files.BaseURL)
	c.Billing.MinBalanceCents = setInt64("GATEWAY_BILLING__MIN_BALANCE_CENTS", c.Billing.MinBalanceCents)
	c.Billing.CharsPerToken = setInt("GATEWAY_BILLING__CHARS_PER_TOKEN", c.Billing.CharsPerToken)
	// ReqLog: 线上可经环境变量紧急开关/调采样,无需改 yaml 重启。
	c.ReqLog.Enabled = setBool("GATEWAY_REQ_LOG__ENABLED", c.ReqLog.Enabled)
	c.ReqLog.SampleRate = setFloat("GATEWAY_REQ_LOG__SAMPLE_RATE", c.ReqLog.SampleRate)
	c.ReqLog.MaxBodyBytes = setInt("GATEWAY_REQ_LOG__MAX_BODY_BYTES", c.ReqLog.MaxBodyBytes)
	c.ReqLog.RetainDays = setInt("GATEWAY_REQ_LOG__RETAIN_DAYS", c.ReqLog.RetainDays)
	c.ReqLog.LogBodies = setBool("GATEWAY_REQ_LOG__LOG_BODIES", c.ReqLog.LogBodies)
	c.Defaults.DefaultProvider = setStr("GATEWAY_DEFAULTS__DEFAULT_PROVIDER", c.Defaults.DefaultProvider)
	c.Web.UserDist = setStr("GATEWAY_WEB__USER_DIST", c.Web.UserDist)
	c.Web.AdminDist = setStr("GATEWAY_WEB__ADMIN_DIST", c.Web.AdminDist)
	c.Edge.Addr = setStr("GATEWAY_EDGE__ADDR", c.Edge.Addr)
	c.Edge.Standalone = setBool("GATEWAY_EDGE__STANDALONE", c.Edge.Standalone)
	c.Edge.ReadTimeout = setStr("GATEWAY_EDGE__READ_TIMEOUT", c.Edge.ReadTimeout)
	c.Edge.WriteTimeout = setStr("GATEWAY_EDGE__WRITE_TIMEOUT", c.Edge.WriteTimeout)
	// 支付: 仅映射关键密钥字段(其余通过 yaml 配置即可)。
	c.Payment.BaseURL = setStr("GATEWAY_PAYMENT__BASE_URL", c.Payment.BaseURL)
	c.Payment.Mock = setBool("GATEWAY_PAYMENT__MOCK", c.Payment.Mock)
	c.Payment.Wechat.AppID = setStr("GATEWAY_PAYMENT__WECHAT__APPID", c.Payment.Wechat.AppID)
	c.Payment.Wechat.MchID = setStr("GATEWAY_PAYMENT__WECHAT__MCHID", c.Payment.Wechat.MchID)
	c.Payment.Wechat.MchSerialNo = setStr("GATEWAY_PAYMENT__WECHAT__MCH_SERIAL_NO", c.Payment.Wechat.MchSerialNo)
	c.Payment.Wechat.PrivateKey = setStr("GATEWAY_PAYMENT__WECHAT__PRIVATE_KEY", c.Payment.Wechat.PrivateKey)
	c.Payment.Wechat.APIv3Key = setStr("GATEWAY_PAYMENT__WECHAT__API_V3_KEY", c.Payment.Wechat.APIv3Key)
	c.Payment.Alipay.AppID = setStr("GATEWAY_PAYMENT__ALIPAY__APPID", c.Payment.Alipay.AppID)
	c.Payment.Alipay.PrivateKey = setStr("GATEWAY_PAYMENT__ALIPAY__PRIVATE_KEY", c.Payment.Alipay.PrivateKey)
	c.Payment.Alipay.AlipayPublicKey = setStr("GATEWAY_PAYMENT__ALIPAY__ALIPAY_PUBLIC_KEY", c.Payment.Alipay.AlipayPublicKey)
}

// ParseFlagConfig 兼容 -config 指定路径: 若指定路径不存在则回退到 config.local.yaml / config.yaml。
func ResolveConfigPath(explicit string) string {
	if explicit != "" {
		if fileExists(explicit) {
			return explicit
		}
	}
	for _, c := range []string{"config.local.yaml", "config.yaml"} {
		if fileExists(c) {
			return c
		}
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
