package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
)

// testDSN 按优先级解析测试库 DSN: GATEWAY_TEST_DSN > 由 GATEWAY_POSTGRES__* 组合(与 CI 一致)。
// 二者皆无时返回空串,调用方 t.Skip 跳过,保证无 DB 环境(如纯 unit 跑)不失败。
func testDSN() string {
	if dsn := os.Getenv("GATEWAY_TEST_DSN"); dsn != "" {
		return dsn
	}
	host := os.Getenv("GATEWAY_POSTGRES__HOST")
	user := os.Getenv("GATEWAY_POSTGRES__USER")
	pw := os.Getenv("GATEWAY_POSTGRES__PASSWORD")
	db := os.Getenv("GATEWAY_POSTGRES__DATABASE")
	if host == "" || db == "" {
		return ""
	}
	return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", user, pw, host, db)
}

// newTestStore 建立到测试库的连接并应用全部迁移,返回就绪的 *Store。
// 无 DB 时跳过该测试。注意: 调用方应使用唯一 ID 隔离数据,不依赖表清空。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := testDSN()
	if dsn == "" {
		t.Skip("skipping store integration test: set GATEWAY_TEST_DSN or GATEWAY_POSTGRES__* to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn, 4)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := s.MigrateUp(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return s
}

// seedUser 插入一条余额为 balanceCents 的测试用户,返回其 id(带唯一后缀防互扰)。
// 同时确保 tenantID 行存在(外键约束);幂等,可重复调用同租户。
func seedUser(t *testing.T, s *Store, tenantID string, balanceCents int64) string {
	t.Helper()
	uid := storeID()
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx,
		`INSERT INTO tenants(id,name,slug,status,created_at) VALUES($1,$2,$2,'active',now()) ON CONFLICT (id) DO NOTHING`,
		tenantID, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if err := s.CreateUser(ctx, &model.User{
		ID: uid, TenantID: tenantID, Email: uid + "@test.local", PasswordHash: "x",
		Role: model.RoleMember, Status: "active", BalanceCents: balanceCents, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// CreateUser 未带初始余额字段时强制对齐期望余额(防默认 0 干扰透支用例)。
	if _, err := s.Pool.Exec(ctx, `UPDATE users SET balance_cents=$2 WHERE id=$1`, uid, balanceCents); err != nil {
		t.Fatalf("set balance: %v", err)
	}
	return uid
}

// TestChargeAtomicIdempotent 验证计费幂等: 同一 request_id 的 usage 第二次扣减不重复,
// 直接返回当前余额。这是防双扣/重复计费的核心不变量(配合部分唯一索引兜底并发)。
func TestChargeAtomicIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-" + storeID()
	uid := seedUser(t, s, tenant, 10000) // 100 元
	reqID := "req-" + storeID()

	charge := func() *model.BillingLedger {
		return &model.BillingLedger{
			ID: storeID(), TenantID: tenant, UserID: uid, RequestID: reqID, Model: "m",
			InputTokens: 10, OutputTokens: 5, CostCents: 1, PriceCents: 300, MarginCents: 299,
			Type: model.LedgerUsage, CreatedAt: time.Now(),
		}
	}

	_, bal1, err := s.ChargeAtomic(ctx, charge())
	if err != nil {
		t.Fatalf("first charge: %v", err)
	}
	if bal1 != 9700 {
		t.Fatalf("after first charge want 9700, got %d", bal1)
	}
	// 同 request_id 再扣一次: 必须幂等,余额不再减少。
	_, bal2, err := s.ChargeAtomic(ctx, charge())
	if err != nil {
		t.Fatalf("idempotent charge: %v", err)
	}
	if bal2 != 9700 {
		t.Fatalf("idempotent re-charge want 9700, got %d", bal2)
	}
}

// TestChargeAtomicConcurrentSameRequestID 验证并发同 request_id 不双扣(资金核心不变量)。
// 攻击向量: 客户端可控的 X-Request-Id 两个并发请求,原实现幂等 SELECT 在 FOR UPDATE 之前,
// 两事务都看不到对方未提交的账目,各自扣款后第二个 INSERT 被唯一索引 ON CONFLICT 吞掉,
// 导致余额扣 N 次但账本只 1 条。修复后幂等闸改为 INSERT...ON CONFLICT DO NOTHING RETURNING,
// 仅本次真正记账才扣余额。
func TestChargeAtomicConcurrentSameRequestID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-" + storeID()
	const startBalance = int64(10000)
	uid := seedUser(t, s, tenant, startBalance)
	reqID := "req-" + storeID()
	const price = int64(300)

	const N = 20
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.ChargeAtomic(ctx, &model.BillingLedger{
				ID: storeID(), TenantID: tenant, UserID: uid, RequestID: reqID, Model: "m",
				InputTokens: 10, OutputTokens: 5, CostCents: 1, PriceCents: price, MarginCents: price - 1,
				Type: model.LedgerUsage, CreatedAt: time.Now(),
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent charge: %v", err)
	}

	// 核心不变量: 无论多少并发,同 request_id 只扣一次。
	u, err := s.GetUser(ctx, uid)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if want := startBalance - price; u.BalanceCents != want {
		t.Fatalf("concurrent double-charge: want %d, got %d (deducted ~%d times)",
			want, u.BalanceCents, (startBalance-u.BalanceCents)/price)
	}
	var cnt int
	if err := s.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM billing_ledger WHERE request_id=$1 AND type='usage'`, reqID).Scan(&cnt); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("want 1 ledger row for request_id, got %d", cnt)
	}
}

// TestChargeAtomicOverdraftProtection 验证透支防护: 余额 < 应收时扣到 0,
// 配合 users.balance_cents>=0 CHECK 保证余额非负;账目仍按完整应收价记录以保收入统计。
func TestChargeAtomicOverdraftProtection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-" + storeID()
	uid := seedUser(t, s, tenant, 100) // 1 元

	_, bal, err := s.ChargeAtomic(ctx, &model.BillingLedger{
		ID: storeID(), TenantID: tenant, UserID: uid, RequestID: "req-" + storeID(), Model: "m",
		InputTokens: 1, OutputTokens: 1, CostCents: 1, PriceCents: 5000, MarginCents: 4999,
		Type: model.LedgerUsage, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("overdraft charge: %v", err)
	}
	if bal != 0 {
		t.Fatalf("overdraft want balance 0, got %d", bal)
	}
	u, err := s.GetUser(ctx, uid)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.BalanceCents != 0 {
		t.Fatalf("persisted balance want 0, got %d", u.BalanceCents)
	}
}

// TestRedeemCodeAtomic 验证卡密兑换: 兑换加余额+写账目;重复兑换/无效码返 ErrInvalidRedeemCode,余额不变。
func TestRedeemCodeAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-" + storeID()
	uid := seedUser(t, s, tenant, 0) // 0 元起步
	code := "RD-" + storeID()
	if err := s.CreateRedeemCode(ctx, &model.RedeemCode{
		ID: storeID(), Code: code, AmountCents: 5000, Status: "active", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create redeem code: %v", err)
	}
	// 首次兑换: +50 元,余额 5000。
	amount, nb, err := s.RedeemCodeAtomic(ctx, code, uid, tenant)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if amount != 5000 || nb != 5000 {
		t.Fatalf("want amount=5000 balance=5000, got amount=%d balance=%d", amount, nb)
	}
	// 重复兑换同一码: 必须失败,余额不变(原子幂等)。
	if _, _, err := s.RedeemCodeAtomic(ctx, code, uid, tenant); !errors.Is(err, ErrInvalidRedeemCode) {
		t.Fatalf("re-redeem want ErrInvalidRedeemCode, got %v", err)
	}
	u, err := s.GetUser(ctx, uid)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.BalanceCents != 5000 {
		t.Fatalf("balance changed after invalid re-redeem: want 5000, got %d", u.BalanceCents)
	}
	// 无效码: ErrInvalidRedeemCode,余额不变。
	if _, _, err := s.RedeemCodeAtomic(ctx, "NOPE-"+storeID(), uid, tenant); !errors.Is(err, ErrInvalidRedeemCode) {
		t.Fatalf("invalid code want ErrInvalidRedeemCode, got %v", err)
	}
}

// TestTransferAtomic 验证团队内团长→成员转账: 同租户正常转账借记/贷记正确;
// 余额不足拒绝;跨租户拒绝;转给自己拒绝。
func TestTransferAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-" + storeID()
	other := "t-" + storeID()
	from := seedUser(t, s, tenant, 10000) // 100 元
	to := seedUser(t, s, tenant, 0)

	// 正常转账 3000 分。
	fb, tb, err := s.TransferAtomic(ctx, tenant, from, to, 3000)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if fb != 7000 || tb != 3000 {
		t.Fatalf("transfer want from=7000 to=3000, got from=%d to=%d", fb, tb)
	}

	// 余额不足(from 仅剩 7000)转 8000 拒绝。
	if _, _, err := s.TransferAtomic(ctx, tenant, from, to, 8000); err == nil {
		t.Fatal("insufficient transfer must error")
	}

	// 跨租户拒绝: 把 to 视作另一租户成员(from 锁同租户,to 取不到行→scan 失败)。
	if _, _, err := s.TransferAtomic(ctx, other, from, to, 100); err == nil {
		t.Fatal("cross-tenant transfer must error")
	}

	// 转给自己拒绝。
	if _, _, err := s.TransferAtomic(ctx, tenant, from, from, 100); err == nil {
		t.Fatal("transfer to self must error")
	}
}

// TestAcceptInviteAtomic 验证邀请接受的原子性: 正常接受→新用户入正确租户/角色;
// 重复接受同一 token、过期 token 均拒绝(ErrInviteUnavailable)。防邀请重放与越权加入。
func TestAcceptInviteAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-" + storeID()
	seedUser(t, s, tenant, 0) // 确保租户存在(外键)
	creator := "creator-" + storeID()
	hash := crypto.APIKeyHash("invite-" + storeID())
	if err := s.CreateInvite(ctx, &model.InviteToken{
		ID: storeID(), TokenHash: hash, TenantID: tenant, Role: "admin",
		CreatedBy: creator, ExpiresAt: time.Now().Add(24 * time.Hour), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	// 正常接受 → 新用户归属正确租户、角色为 admin。
	inv, uid, err := s.AcceptInviteAtomic(ctx, hash, "new-"+storeID()+"@x.com", "pwhash", time.Now())
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if inv.TenantID != tenant {
		t.Fatalf("invite tenant want %s, got %s", tenant, inv.TenantID)
	}
	u, _ := s.GetUser(ctx, uid)
	if u.TenantID != tenant || string(u.Role) != "admin" {
		t.Fatalf("new user want %s/admin, got %s/%s", tenant, u.TenantID, u.Role)
	}
	// 重复接受同一 token → 拒绝(已 used)。
	if _, _, err := s.AcceptInviteAtomic(ctx, hash, "dup@x.com", "pwhash", time.Now()); !errors.Is(err, ErrInviteUnavailable) {
		t.Fatalf("re-accept want ErrInviteUnavailable, got %v", err)
	}
	// 过期 token → 拒绝。
	expHash := crypto.APIKeyHash("expired-" + storeID())
	s.CreateInvite(ctx, &model.InviteToken{
		ID: storeID(), TokenHash: expHash, TenantID: tenant, Role: "member",
		CreatedBy: creator, ExpiresAt: time.Now().Add(-time.Hour), CreatedAt: time.Now(),
	})
	if _, _, err := s.AcceptInviteAtomic(ctx, expHash, "exp@x.com", "pwhash", time.Now()); !errors.Is(err, ErrInviteUnavailable) {
		t.Fatalf("expired want ErrInviteUnavailable, got %v", err)
	}
}

// TestSettlePaymentAtomic 验证支付入账幂等: pending→paid 仅入账一次(余额加一次),
// 重复 settle 返回 false 且余额不再变化。防回调重放导致重复充值。
func TestSettlePaymentAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tenant := "t-" + storeID()
	uid := seedUser(t, s, tenant, 0)
	trade := "trade-" + storeID()
	// transaction_id 必须每次运行唯一,否则撞 uniq_payment_orders_provider_txn 历史数据。
	txn1 := "txn-" + storeID()
	txn2 := "txn2-" + storeID()
	if err := s.CreatePaymentOrder(ctx, &model.PaymentOrder{
		ID: storeID(), TenantID: tenant, UserID: uid, OutTradeNo: trade, Provider: "mock",
		AmountCents: 5000, Status: "pending", ExpiresAt: time.Now().Add(time.Hour), CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create order: %v", err)
	}
	// 首次 settle → true, 余额 += 5000。
	ok, nb, err := s.SettlePaymentAtomic(ctx, trade, txn1, time.Now())
	if err != nil || !ok {
		t.Fatalf("settle want ok=true, got ok=%v err=%v", ok, err)
	}
	if nb != 5000 {
		t.Fatalf("balance want 5000, got %d", nb)
	}
	// 重复 settle(模拟回调重放) → false, 余额不变。
	ok2, _, err := s.SettlePaymentAtomic(ctx, trade, txn2, time.Now())
	if err != nil || ok2 {
		t.Fatalf("re-settle want ok=false(幂等), got ok=%v err=%v", ok2, err)
	}
	u, _ := s.GetUser(ctx, uid)
	if u.BalanceCents != 5000 {
		t.Fatalf("balance after replay want 5000, got %d", u.BalanceCents)
	}
}
