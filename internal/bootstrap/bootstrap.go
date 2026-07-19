// Package bootstrap 装配共享依赖(store/redis/providers/cipher/auth/billing/relay/files),
// 供控制面(cmd/gateway)与数据面(cmd/edge)两个二进制复用,避免重复初始化代码。
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aitoys/llm-gateway/internal/api/anthropic"
	"github.com/aitoys/llm-gateway/internal/api/openai"
	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/billing"
	"github.com/aitoys/llm-gateway/internal/config"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/files"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/metrics"
	"github.com/aitoys/llm-gateway/internal/middleware"
	"github.com/aitoys/llm-gateway/internal/payment"
	"github.com/aitoys/llm-gateway/internal/provider"
	"github.com/aitoys/llm-gateway/internal/provider/mock"
	"github.com/aitoys/llm-gateway/internal/provider/openaicomp"
	"github.com/aitoys/llm-gateway/internal/relay"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Deps 已装配的共享依赖。
type Deps struct {
	Cfg       *config.Config
	Store     *store.Store
	RDB       *redis.Client
	Cipher    *crypto.Cipher
	Auth      *auth.Service
	Billing   *billing.Service
	Providers *provider.Registry
	Relay     *relay.Service
	Files     *files.Service
	Payment   *payment.Service
}

// Build 装配全部共享依赖(不负责迁移/seed,由调用方决定)。
func Build(cfg *config.Config) (*Deps, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.Dev {
		log.Printf("[WARN] dev=true: 密钥校验已放宽、模拟充值/注册已放开,严禁用于生产")
	}
	logging.Init(cfg.Log.Format, cfg.Log.Level)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	st, err := store.Open(ctx, cfg.Postgres.DSN(), cfg.Postgres.MaxConns)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB})
	// 启动即探活 Redis:不可达仅告警(限流/熔断会降级),不阻断启动。
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("[WARN] redis 不可达(%v):限流/熔断/缓存将降级,请检查配置", err)
	}

	registry := provider.NewRegistry()
	registry.Register(mock.New())
	registry.Register(openaicomp.New("bailian", "https://dashscope.aliyuncs.com/compatible-mode/v1"))
	registry.Register(openaicomp.New("volcark", "https://ark.cn-beijing.volces.com/api/v3"))
	registry.Register(openaicomp.New("qianfan", "https://qianfan.baidubce.com/v2"))
	// DeepSeek、智谱 GLM 为独立 openai 兼容供应商,直连其官方 API。
	registry.Register(openaicomp.New("deepseek", "https://api.deepseek.com"))
	registry.Register(openaicomp.New("zhipuai", "https://open.bigmodel.cn/api/paas/v4"))

	cipher, err := crypto.NewCipher(cfg.Auth.ChannelKeyMaster)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("cipher: %w", err)
	}
	authSvc := auth.New(cfg.Auth.JWTSecret, auth.ParseTTL(cfg.Auth.AccessTTL, 15*time.Minute))
	billingSvc := billing.New(st)
	relaySvc := &relay.Service{
		Store: st, Providers: registry, Billing: billingSvc,
		Cipher: cipher, DefaultProvider: cfg.Defaults.DefaultProvider,
		Breaker:         relay.NewRedisBreaker(rdb, 3, 60*time.Second),
		RDB:             rdb,
		MinBalanceCents: cfg.Billing.MinBalanceCents,
		CharsPerToken:   cfg.Billing.CharsPerToken,
		ReqLog: relay.ReqLogCfg{
			Enabled: cfg.ReqLog.Enabled, SampleRate: cfg.ReqLog.SampleRate,
			MaxBodyBytes: cfg.ReqLog.MaxBodyBytes, LogBodies: cfg.ReqLog.LogBodies,
		},
	}
	fileSvc, err := files.New(st, cfg.Files.LocalRoot, cfg.Files.BaseURL)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("files: %w", err)
	}

	paySvc := buildPayment(cfg, st, billingSvc)

	return &Deps{
		Cfg: cfg, Store: st, RDB: rdb, Cipher: cipher, Auth: authSvc,
		Billing: billingSvc, Providers: registry, Relay: relaySvc, Files: fileSvc,
		Payment: paySvc,
	}, nil
}

// buildPayment 按 config 构造支付服务并注册启用的渠道。
// mock=true 时仅注册 mock(开发/测试,不依赖商户资质);否则按各渠道 enabled + 密钥完整性注册。
// 单个渠道构造失败仅告警并跳过,不阻断启动(其余渠道仍可用)。
func buildPayment(cfg *config.Config, st *store.Store, b *billing.Service) *payment.Service {
	svc := payment.New(st, b, cfg.Payment.BaseURL, cfg.Payment.ExpiresMin)
	// mock=true: 仅 mock,不依赖商户资质(纯开发/测试)。
	// dev(非 mock): 注册 mock 保底,再叠加已配置的真实渠道(便于本地用真实沙箱联调)。
	if cfg.Payment.Mock {
		svc.Register(payment.NewMock())
		return svc
	}
	if cfg.Dev {
		svc.Register(payment.NewMock())
	}
	if cfg.Payment.Wechat.Enabled {
		w, err := payment.NewWechat(cfg.Payment.Wechat.AppID, cfg.Payment.Wechat.MchID,
			cfg.Payment.Wechat.MchSerialNo, cfg.Payment.Wechat.PrivateKey, cfg.Payment.Wechat.APIv3Key)
		if err != nil {
			log.Printf("[WARN] 微信支付渠道装配失败,已跳过: %v", err)
		} else {
			svc.Register(w)
		}
	}
	if cfg.Payment.Alipay.Enabled {
		a, err := payment.NewAlipay(cfg.Payment.Alipay.AppID, cfg.Payment.Alipay.PrivateKey,
			cfg.Payment.Alipay.AlipayPublicKey, !cfg.Payment.Alipay.Sandbox)
		if err != nil {
			log.Printf("[WARN] 支付宝渠道装配失败,已跳过: %v", err)
		} else {
			svc.Register(a)
		}
	}
	return svc
}

// edgeCORS 构造 edge 接入点 CORS 配置: 仅放行显式配置 Origin,默认不同源不发 CORS 头。
func edgeCORS(cfg *config.Config) cors.Config {
	if len(cfg.Server.CORSOrigins) == 0 {
		if cfg.Dev {
			return cors.Config{
				AllowOriginFunc:  func(string) bool { return true },
				AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
				AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
				AllowCredentials: false,
				MaxAge:           12 * time.Hour,
			}
		}
		return cors.Config{
			AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders: []string{"Authorization", "Content-Type"},
		}
	}
	return cors.Config{
		AllowOrigins:     cfg.Server.CORSOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
}

// Close 释放资源。
func (d *Deps) Close() {
	if d.Store != nil {
		d.Store.Close()
	}
	if d.RDB != nil {
		_ = d.RDB.Close()
	}
}

// ReadyHandler 就绪探针: 同时探测 Postgres 与 Redis,任一不可用返回 503。
// 区别于 /healthz(存活,恒 200),用于 K8s readinessGate/网关前置健康检查。
func (d *Deps) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.Store.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"ok":false,"dep":"postgres"}`)
			return
		}
		if d.RDB != nil {
			if err := d.RDB.Ping(ctx).Err(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"ok":false,"dep":"redis"}`)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}
}

// ApplyTrustedProxies 设置 gin 信任的代理 CIDR,决定 ClientIP() 是否解析 X-Forwarded-For。
// 未配置时传 nil(不信任任何代理,ClientIP=RemoteAddr),避免 XFF 伪造绕过 IP 白名单/限流。
// 调用方在 gin.New() 之后、注册路由之前调用。
func ApplyTrustedProxies(r *gin.Engine, cfg *config.Config) {
	if cfg == nil {
		_ = r.SetTrustedProxies(nil)
		return
	}
	if err := r.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		log.Printf("[WARN] SetTrustedProxies 失败,退化为不信任任何代理: %v", err)
		_ = r.SetTrustedProxies(nil)
	}
}

// EdgeEngine 构造数据面(接入点)gin 引擎:/v1 推理 + /files + 健康检查。
// 控制面与独立 edge 二进制共用此函数,确保接入点行为一致。
func (d *Deps) EdgeEngine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	ApplyTrustedProxies(r, d.Cfg)
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID()) // 最外层注入 request_id,使日志/usage/billing 共享同一链路 ID
	r.Use(logging.Middleware())
	r.Use(cors.New(edgeCORS(d.Cfg)))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/readyz", gin.WrapF(d.ReadyHandler()))
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	oai := openai.Controller{Relay: d.Relay}
	ant := anthropic.Controller{Relay: d.Relay}

	g := r.Group("")
	// 鉴权 + 限流先挂,确保其后的所有写入/推理路由(/v1/files 上传、/v1/* 推理)都受保护。
	// gin 按注册顺序应用 Use,故 Use 必须在路由注册之前。
	g.Use(middleware.APIKeyAuth(d.Store, d.RDB))
	g.Use(middleware.RateLimit(d.RDB))
	g.POST("/v1/files", d.uploadHandler())
	g.POST("/v1/chat/completions", oai.ChatCompletions)
	g.GET("/v1/models", oai.Models)
	g.POST("/v1/embeddings", oai.Embeddings)
	g.POST("/v1/messages", ant.Messages)
	// 文件下载保持公开: 上传返回的 URL 由前端 <img src> 直接引用(浏览器无法带 Authorization)。
	// 安全性靠"文件 ID 为高熵随机串"(事实上的 capability token)。如需更强控制可改签名 URL。
	g.GET("/files/*path", gin.WrapF(d.Files.ServeHTTP()))
	return r
}

// uploadHandler 文件上传(复用 web.playgroundUpload 之外的独立实现,避免引入 web 包)。
func (d *Deps) uploadHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 限制 multipart 内存缓冲,超额落盘;真正的硬上限由 files.Upload 的 LimitReader 兜底。
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, files.MaxUploadBytes+1<<20)
		sub, _ := c.Get("subject")
		s, _ := sub.(auth.Subject)
		fh, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": ginErr(err)})
			return
		}
		src, err := fh.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		defer src.Close()
		f, err := d.Files.Upload(c.Request.Context(), s.TenantID, s.UserID, fh.Filename, fh.Header.Get("Content-Type"), src)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": ginErr(err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": f.ID, "filename": f.Filename, "bytes": f.Size, "url": f.StorageURL, "purpose": f.Purpose})
	}
}

// ginErr 把上传相关错误映射为对用户友好的提示(不泄露内部细节)。
func ginErr(err error) string {
	switch err {
	case nil:
		return ""
	default:
		return err.Error()
	}
}
