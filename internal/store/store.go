// Package store 封装 Postgres 持久化与迁移。
package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store 持久化入口。
type Store struct {
	Pool *pgxpool.Pool
}

// Open 创建连接池。
func Open(ctx context.Context, dsn string, maxConns int) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns) //nolint:gosec // 连接池配置,实际值远小于 int32 上界
	}
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() {
	if s.Pool != nil {
		s.Pool.Close()
	}
}

// Ping 探活数据库连接池(供 /readyz 就绪探针)。
func (s *Store) Ping(ctx context.Context) error {
	return s.Pool.Ping(ctx)
}

// MigrateUp 执行全部未应用的 up 迁移。
func (s *Store) MigrateUp(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}
	versions, err := migrationVersions()
	if err != nil {
		return err
	}
	for _, v := range versions {
		var exists bool
		if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", v).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sql, err := migrationFS.ReadFile("migrations/" + v + ".up.sql")
		if err != nil {
			return err
		}
		if _, err := s.Pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply migration %s: %w", v, err)
		}
		if _, err := s.Pool.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING", v); err != nil {
			return err
		}
	}
	return nil
}

// MigrateDown 回滚最后(全部)迁移。仅用于开发。
func (s *Store) MigrateDown(ctx context.Context) error {
	down, err := migrationFS.ReadFile("migrations/0001_init.down.sql")
	if err != nil {
		return err
	}
	if _, err := s.Pool.Exec(ctx, string(down)); err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

func migrationVersions() ([]string, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{})
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		// 版本号 = 去掉 .up.sql 后的完整 stem,例: 0001_init
		set[strings.TrimSuffix(name, ".up.sql")] = struct{}{}
	}
	versions := make([]string, 0, len(set))
	for v := range set {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions, nil
}
