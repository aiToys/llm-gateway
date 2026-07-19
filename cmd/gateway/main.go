// Package main 是 LLM Gateway 控制面入口(管理端 + Web + 公共展示页)。
// 默认同时内嵌数据面(网关接入点 /v1)于同端口; 配置 edge.standalone=true 时
// 不内嵌接入点,由独立 cmd/edge 二进制承担(真正的双二进制拆分)。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aitoys/llm-gateway/internal/api/web"
	"github.com/aitoys/llm-gateway/internal/billing"
	"github.com/aitoys/llm-gateway/internal/bootstrap"
	"github.com/aitoys/llm-gateway/internal/config"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/metrics"
	"github.com/aitoys/llm-gateway/internal/payment"
	"github.com/aitoys/llm-gateway/internal/static"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/aitoys/llm-gateway/internal/version"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径")
	migrate := flag.String("migrate", "", "up|down, 执行迁移后退出")
	seedFlag := flag.Bool("seed", false, "灌入 mock 种子数据后退出")
	showVersion := flag.Bool("version", false, "打印版本后退出")
	flag.Parse()

	if *showVersion {
		fmt.Println("llm-gateway", version.Version)
		return
	}

	cfg, err := config.Load(config.ResolveConfigPath(*configPath))
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	deps, err := bootstrap.Build(cfg)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	defer deps.Close()

	switch *migrate {
	case "up":
		if err := deps.Store.MigrateUp(context.Background()); err != nil {
			log.Fatalf("migrate up: %v", err)
		}
		log.Println("migrations applied")
		return
	case "down":
		if err := deps.Store.MigrateDown(context.Background()); err != nil {
			log.Fatalf("migrate down: %v", err)
		}
		log.Println("migrations rolled back")
		return
	}
	if err := deps.Store.MigrateUp(context.Background()); err != nil {
		log.Fatalf("auto migrate: %v", err)
	}
	// 确保配置中声明的平台超级管理员存在(首次部署获得跨租户管理权)。
	if err := ensureBootstrapAdmin(deps.Store, cfg); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}
	if *seedFlag {
		if err := seed(deps.Store, deps.Cipher); err != nil {
			log.Fatalf("seed: %v", err)
		}
		log.Println("seed completed")
		return
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	bootstrap.ApplyTrustedProxies(r, cfg)
	r.Use(gin.Recovery())
	r.Use(logging.Middleware())
	r.Use(cors.New(corsConfig(cfg)))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/readyz", gin.WrapF(deps.ReadyHandler()))
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	// 公共展示页(已构建 dist 时)
	if cfg.Web.UserDist != "" || cfg.Web.AdminDist != "" {
		apps := map[string]string{}
		if cfg.Web.UserDist != "" {
			apps["/"] = cfg.Web.UserDist
		}
		if cfg.Web.AdminDist != "" {
			apps["/admin"] = cfg.Web.AdminDist
		}
		static.MountSPAs(r, apps)
	}

	// 控制面 REST
	webServer := &web.Server{Store: deps.Store, Auth: deps.Auth, Billing: deps.Billing, Relay: deps.Relay, Cipher: deps.Cipher, RDB: deps.RDB, FileSvc: deps.Files,
		Payment: deps.Payment, Dev: cfg.Dev, AllowSignup: cfg.Auth.AllowSignup}
	webServer.Register(r)

	// 后台 worker(payment sweeper / billing retry)共用一个可取消 root context,
	// 收到退出信号时先 cancel 让 worker优雅收尾(完成当前 tick 的 in-flight 计费),
	// 再 Shutdown HTTP server,避免靠进程强杀截断进行中的扣款落库。
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// 支付: 超时未支付订单关单(每 1min,先查渠道确认未付再关,避免误关已付单)。
	if deps.Payment != nil {
		go runPaymentSweeper(rootCtx, deps.Payment)
	}
	// 计费: 重试后置计费失败的应扣项(防漏账,每 20s)。
	go runBillingRetryLoop(rootCtx, deps.Billing)
	// 请求日志: 按保留天数清理过期原文日志(防无限膨胀;仅启用时启动,每 1h)。
	if cfg.ReqLog.Enabled && cfg.ReqLog.RetainDays > 0 {
		go runReqLogSweeper(rootCtx, deps.Store, cfg.ReqLog.RetainDays)
	}

	// 数据面(接入点): standalone=true 时不内嵌(由 cmd/edge 承担); 否则内嵌到本进程。
	var edgeSrv *http.Server
	if !cfg.Edge.Standalone {
		edgeEngine := deps.EdgeEngine()
		if cfg.Edge.Addr != "" && cfg.Edge.Addr != cfg.Server.Addr {
			// 同进程,独立端口
			edgeSrv = &http.Server{Addr: cfg.Edge.Addr, Handler: edgeEngine, ReadHeaderTimeout: 10 * time.Second}
		} else {
			// 同端口: 把 edge 路由并到控制面 engine(通过反向代理挂载其 handler)
			r.Any("/v1/*any", gin.WrapH(edgeEngine))
			r.Any("/files/*any", gin.WrapH(edgeEngine))
		}
	}

	srv := &http.Server{Addr: cfg.Server.Addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		mode := "内嵌接入点(同端口)"
		if cfg.Edge.Standalone {
			mode = "纯控制面(接入点由独立 cmd/edge 承担)"
		} else if edgeSrv != nil {
			mode = "内嵌接入点(独立端口 " + cfg.Edge.Addr + ")"
		}
		log.Printf("[control] listening on %s — %s", cfg.Server.Addr, mode)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	if edgeSrv != nil {
		go func() {
			log.Printf("[edge] 接入点 listening on %s", cfg.Edge.Addr)
			if err := edgeSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("edge listen: %v", err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")
	// 先停止后台 worker(完成当前 tick 的 in-flight 计费),再优雅关闭 HTTP server。
	rootCancel()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	if edgeSrv != nil {
		_ = edgeSrv.Shutdown(shutCtx)
	}
	fmt.Println("bye")
}

// runPaymentSweeper 周期关闭超时未支付的订单(防回调丢失导致订单永久挂起)。
// 仿 middleware/ratelimit.go 的 localBuckets.sweep 范式;优雅关闭由进程退出承接。
func runPaymentSweeper(ctx context.Context, svc *payment.Service) {
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			svc.CloseExpired(ctx)
		}
	}
}

// runBillingRetryLoop 周期重试后置计费失败的应扣项(防漏账)。
func runBillingRetryLoop(ctx context.Context, svc *billing.Service) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			svc.RetryPendingCharges(ctx)
		}
	}
}

// runReqLogSweeper 周期清理超过保留天数的请求/响应原文日志(防存储无限膨胀)。
func runReqLogSweeper(ctx context.Context, st *store.Store, retainDays int) {
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	sweep := func() {
		before := time.Now().Add(-time.Duration(retainDays) * 24 * time.Hour)
		if n, err := st.DeleteOldRequestLogs(ctx, before); err == nil && n > 0 {
			logging.L().Info("request logs swept", "deleted", n, "older_than", before.Format(time.RFC3339))
		}
	}
	sweep() // 启动即清一次,避免重启后首次延迟 1h
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}

// corsConfig 构造 CORS 配置。
// 仅放行显式配置的 Origin(cors_origins);未配置时不回显任意 Origin,避免
// "AllowOriginFunc 恒真 + AllowCredentials" 导致的跨域读取风险。
// 开发模式(dev)且未配置时,放宽为任意 Origin 以便前后端分离联调。
func corsConfig(cfg *config.Config) cors.Config {
	origins := cfg.Server.CORSOrigins
	if len(origins) == 0 {
		if cfg.Dev {
			// 开发态: 回显任意 Origin,但不开 Credentials(降低风险)。
			return cors.Config{
				AllowOriginFunc:  func(string) bool { return true },
				AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
				AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
				AllowCredentials: false,
				MaxAge:           12 * time.Hour,
			}
		}
		// 生产默认: 不发送 CORS 头(同源访问)。
		return cors.Config{
			AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowHeaders: []string{"Authorization", "Content-Type"},
		}
	}
	allow := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allow[o] = struct{}{}
	}
	return cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
		AllowOriginFunc: func(o string) bool { // 双保险: AllowOrigins + 函数校验
			_, ok := allow[o]
			return ok
		},
	}
}
