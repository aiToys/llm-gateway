package store

import (
	"context"
	"testing"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
)

// TestRequestLogInsertListGet 覆盖请求日志的插入、列表过滤、详情、TTL 清理。
// 验证: 列表不返回 body(可能很大)、详情返回 body、过滤生效、DeleteOld 仅清过期。
func TestRequestLogInsertListGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "tnt-reqlog-" + storeID()
	uid := seedUser(t, s, tenant, 0)

	// 插入 3 条: 2 条该租户(1 成功 1 失败)、1 条别的租户(验证过滤)。
	mk := func(reqID, modelName, status string, body string) *model.RequestLog {
		l := &model.RequestLog{
			ID: storeID(), RequestID: reqID, TenantID: tenant, UserID: uid,
			Model: modelName, Provider: "mock", ChannelID: "ch1", Status: 200,
			InputTokens: 5, OutputTokens: 10, PriceCents: 30, CreatedAt: time.Now(),
		}
		if body != "" {
			l.RequestBody = &body
		}
		if status == "error" {
			l.Status = 502
			e := "upstream 500"
			l.Error = &e
		}
		return l
	}
	if err := s.InsertRequestLog(ctx, mk("req-A", "qwen3-max", "ok", `{"hi":1}`)); err != nil {
		t.Fatalf("insert A: %v", err)
	}
	if err := s.InsertRequestLog(ctx, mk("req-B", "glm-5.2", "error", "")); err != nil {
		t.Fatalf("insert B: %v", err)
	}
	other := mk("req-X", "qwen3-max", "ok", "")
	other.TenantID = "tnt-other-" + storeID()
	if err := s.InsertRequestLog(ctx, other); err != nil {
		t.Fatalf("insert X: %v", err)
	}

	// 列表: 按租户过滤 → 应仅 2 条(req-A/req-B);且不含 body 字段。
	rows, err := s.ListRequestLogs(ctx, ReqLogFilter{TenantID: tenant}, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows got %d", len(rows))
	}
	// 倒序: B 先(更晚插入)。
	if rows[0].RequestID != "req-B" {
		t.Fatalf("order: %+v", rows[0].RequestID)
	}
	// 列表不返回 body。
	if rows[0].RequestBody != nil {
		t.Fatalf("list must not return request_body, got %v", rows[0].RequestBody)
	}

	// 过滤: 按 model=qwen3-max → 仅 req-A(同租户)。
	rowsM, _ := s.ListRequestLogs(ctx, ReqLogFilter{TenantID: tenant, Model: "qwen3-max"}, 0)
	if len(rowsM) != 1 || rowsM[0].RequestID != "req-A" {
		t.Fatalf("model filter: %+v", rowsM)
	}
	// 过滤: 按 status=502 → 仅 req-B。
	rowsE, _ := s.ListRequestLogs(ctx, ReqLogFilter{TenantID: tenant, Status: 502}, 0)
	if len(rowsE) != 1 || rowsE[0].RequestID != "req-B" {
		t.Fatalf("status filter: %+v", rowsE)
	}

	// 详情: 返回 body。
	full, err := s.GetRequestLog(ctx, rows[1].ID) // req-A
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if full.RequestBody == nil || *full.RequestBody != `{"hi":1}` {
		t.Fatalf("detail body: %v", full.RequestBody)
	}
	if full.RequestID != "req-A" {
		t.Fatalf("detail req id: %s", full.RequestID)
	}

	// limit clamp: 传超限值(>1000)应回退默认 200,不报错。
	if _, err := s.ListRequestLogs(ctx, ReqLogFilter{TenantID: tenant}, 99999); err != nil {
		t.Fatalf("limit clamp: %v", err)
	}
}

// TestDeleteOldRequestLogs 验证 TTL 清理仅删早于 before 的记录,保留新记录。
func TestDeleteOldRequestLogs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "tnt-reqlog-old-" + storeID()
	uid := seedUser(t, s, tenant, 0)

	old := &model.RequestLog{
		ID: storeID(), RequestID: "req-old", TenantID: tenant, UserID: uid,
		Model: "m", Status: 200, CreatedAt: time.Now().Add(-48 * time.Hour),
	}
	fresh := &model.RequestLog{
		ID: storeID(), RequestID: "req-fresh", TenantID: tenant, UserID: uid,
		Model: "m", Status: 200, CreatedAt: time.Now(),
	}
	if err := s.InsertRequestLog(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertRequestLog(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	// 清理 24h 前的: old 应删,fresh 保留。
	n, err := s.DeleteOldRequestLogs(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("delete old: %v", err)
	}
	if n == 0 {
		t.Fatal("want at least 1 deleted")
	}
	rows, _ := s.ListRequestLogs(ctx, ReqLogFilter{TenantID: tenant}, 0)
	if len(rows) != 1 || rows[0].RequestID != "req-fresh" {
		t.Fatalf("after sweep want only fresh got %+v", rows)
	}
}
