// Package main 是 LLM Gateway 数据面(网关接入点)独立二进制。
// 仅装配 relay/providers/files,提供 /v1 推理 + /files 上传下载,无状态可横向扩展。
// 与控制面 cmd/gateway 共享同一 Postgres/Redis; 真正的双二进制拆分形态。
//
// 典型部署:
//
//	cmd/edge   -config config.yaml            # 接入点,公网暴露,多副本 + LB
//	cmd/gateway -config config.yaml           # 控制面(配置 edge.standalone=true),内网
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

	"github.com/aitoys/llm-gateway/internal/bootstrap"
	"github.com/aitoys/llm-gateway/internal/config"
	"github.com/aitoys/llm-gateway/internal/version"
)

func main() {
	configPath := flag.String("config", "", "配置文件路径")
	showVersion := flag.Bool("version", false, "打印版本后退出")
	flag.Parse()

	if *showVersion {
		fmt.Println("llm-gateway-edge", version.Version)
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

	// edge 不负责迁移/seed(由控制面管理),但确保 schema 存在(幂等)。
	if err := deps.Store.MigrateUp(context.Background()); err != nil {
		log.Fatalf("auto migrate: %v", err)
	}

	addr := cfg.Edge.Addr
	if addr == "" {
		addr = cfg.Server.Addr // 未单独配置则回退 server.addr
	}

	srv := &http.Server{Addr: addr, Handler: deps.EdgeEngine(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("[edge] 接入点(独立二进制) listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("edge shutting down...")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	fmt.Println("bye")
}
