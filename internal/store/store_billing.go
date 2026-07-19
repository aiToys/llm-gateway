package store

import (
	"context"
	"errors"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// ChargeAtomic 在单个事务内完成"扣减余额 + 写账目",并对用户行加 FOR UPDATE 行锁。
// 保证: ① 余额变更与账目要么同时提交要么同时回滚(避免"钱扣了没账目"的资金凭空消失);
//
//	② 并发请求串行扣款,balance_after 快照单调可信,审计可对账。
//
// 透支防护: 实扣 = min(余额, 应收),不足时扣到 0(配合 users_balance_nonnegative
//
//	CHECK 约束,余额下界恒为 0);账目仍按完整应收价 price_cents 记录以保收入统计。
//	请求前的预检 preflight 负责尽早拦截明显无力支付的调用。
func (s *Store) ChargeAtomic(ctx context.Context, l *model.BillingLedger) (int64, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// FOR UPDATE 锁用户行,串行化并发扣款并取到可信快照。
	// 必须在幂等判断之前:否则两个并发事务的幂等 SELECT 都看不到对方未提交的行,
	// 各自通过锁后都会 UPDATE 扣款,而第二个 INSERT 被唯一索引 ON CONFLICT 吞掉,
	// 导致余额扣两次但账本只落一条(账实断裂)。X-Request-Id 由客户端控制,可被外部触发。
	var prev int64
	if err = tx.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id=$1 FOR UPDATE`, l.UserID).Scan(&prev); err != nil {
		return 0, err
	}

	// 实扣 = min(余额, 应收);不足时扣到 0,配合 CHECK 约束保证余额非负。账目仍记完整应收价。
	deduct := l.PriceCents
	if deduct > prev {
		deduct = prev
	}
	nb := prev - deduct
	l.BalanceAfter = nb

	// 幂等闸(原子): usage 类型先尝试 INSERT 账目(ON CONFLICT DO NOTHING + RETURNING)。
	// 仅当本次真正 INSERT 成功(RETURNING 有行)才扣减余额——彻底消除"检查→扣款→记账冲突被吞"的双扣窗口,
	// 唯一索引 uniq_ledger_usage_request 成为并发幂等的唯一闸门。
	if l.Type == model.LedgerUsage && l.RequestID != "" {
		var insertedID string
		err = tx.QueryRow(ctx,
			`INSERT INTO billing_ledger(id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cost_cents,price_cents,margin_cents,type,balance_after,created_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			 ON CONFLICT DO NOTHING
			 RETURNING id`,
			l.ID, l.TenantID, l.UserID, l.RequestID, l.Model, l.InputTokens, l.OutputTokens,
			l.CostCents, l.PriceCents, l.MarginCents, l.Type, l.BalanceAfter, l.CreatedAt).Scan(&insertedID)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return 0, err
			}
			// RETURNING 无行 = 唯一冲突 = 该 request_id 已计费:不扣减,返回当前余额。
			if err = tx.Commit(ctx); err != nil {
				return 0, err
			}
			return prev, nil
		}
		// 本次真正记账 → 扣减余额。
		if _, err = tx.Exec(ctx, `UPDATE users SET balance_cents=$2 WHERE id=$1`, l.UserID, nb); err != nil {
			return 0, err
		}
		if err = tx.Commit(ctx); err != nil {
			return 0, err
		}
		return nb, nil
	}

	// 非 usage 类型(充值/退款/转账等不经此路径,保留兜底):扣减 + 记账,无幂等闸。
	if _, err = tx.Exec(ctx, `UPDATE users SET balance_cents=$2 WHERE id=$1`, l.UserID, nb); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO billing_ledger(id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cost_cents,price_cents,margin_cents,type,balance_after,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		l.ID, l.TenantID, l.UserID, l.RequestID, l.Model, l.InputTokens, l.OutputTokens,
		l.CostCents, l.PriceCents, l.MarginCents, l.Type, l.BalanceAfter, l.CreatedAt); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return nb, nil
}

// AdjustAtomic 在单事务内"改余额 + (可选)写充值记录 + 写账目",保证账实一致。
// l.PriceCents 为负数表示加余额(充值/退款);加余额不会触发非负 CHECK。
func (s *Store) AdjustAtomic(ctx context.Context, l *model.BillingLedger, recharge *model.Recharge) (int64, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var nb int64
	if err := tx.QueryRow(ctx,
		`UPDATE users SET balance_cents = balance_cents + $2 WHERE id = $1 RETURNING balance_cents`,
		l.UserID, -l.PriceCents).Scan(&nb); err != nil {
		return 0, err
	}
	l.BalanceAfter = nb
	if recharge != nil {
		if _, err := tx.Exec(ctx,
			`INSERT INTO recharges(id,tenant_id,user_id,amount_cents,status,created_at) VALUES($1,$2,$3,$4,$5,$6)`,
			recharge.ID, recharge.TenantID, recharge.UserID, recharge.AmountCents, recharge.Status, recharge.CreatedAt); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO billing_ledger(id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cost_cents,price_cents,margin_cents,type,balance_after,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		l.ID, l.TenantID, l.UserID, l.RequestID, l.Model, l.InputTokens, l.OutputTokens,
		l.CostCents, l.PriceCents, l.MarginCents, l.Type, l.BalanceAfter, l.CreatedAt); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return nb, nil
}

// TransferAtomic 团队内团长→成员的余额转账: 单事务借记 from / 贷记 to + 写 2 条 transfer 账目。
// 校验: 双方同租户(tenantID)、from 余额足够。按 user id 升序加锁防死锁。
// 返回转账后的 (fromBalance, toBalance)。账本仍是唯一资金真相,可对账。
func (s *Store) TransferAtomic(ctx context.Context, tenantID, fromID, toID string, amountCents int64) (int64, int64, error) {
	if amountCents <= 0 {
		return 0, 0, errors.New("amount must be positive")
	}
	// 上界防 int64 溢出绕过非负 CHECK(单笔不超过 1e12 分 = 100 亿元)。
	if amountCents > 1_000_000_000_000 {
		return 0, 0, errors.New("amount too large")
	}
	if fromID == toID {
		return 0, 0, errors.New("cannot transfer to self")
	}
	// 按 id 升序加锁,避免两个反向转账互锁。
	first, second := fromID, toID
	if first > second {
		first, second = second, first
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	var fb int64
	if err = tx.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, first, tenantID).Scan(&fb); err != nil {
		return 0, 0, err
	}
	var sb int64
	if err = tx.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, second, tenantID).Scan(&sb); err != nil {
		return 0, 0, err
	}
	// 取到双方余额后按 from/to 计算。
	fromBal, toBal := fb, sb
	if first == toID {
		fromBal, toBal = sb, fb
	}
	if fromBal < amountCents {
		return 0, 0, errors.New("余额不足")
	}
	fromBal -= amountCents
	toBal += amountCents
	if _, err = tx.Exec(ctx, `UPDATE users SET balance_cents=$2 WHERE id=$1`, fromID, fromBal); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET balance_cents=$2 WHERE id=$1`, toID, toBal); err != nil {
		return 0, 0, err
	}
	now := time.Now()
	// 出账(price 正) + 入账(price 负),成对记录,margin 为 0(团队内部无毛利)。
	for _, l := range []*model.BillingLedger{
		{ID: storeID(), TenantID: tenantID, UserID: fromID, RequestID: "transfer", Model: "-", CostCents: 0, PriceCents: amountCents, MarginCents: 0, Type: model.LedgerTransfer, BalanceAfter: fromBal, CreatedAt: now},
		{ID: storeID(), TenantID: tenantID, UserID: toID, RequestID: "transfer", Model: "-", CostCents: 0, PriceCents: -amountCents, MarginCents: 0, Type: model.LedgerTransfer, BalanceAfter: toBal, CreatedAt: now},
	} {
		if _, err = tx.Exec(ctx,
			`INSERT INTO billing_ledger(id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cost_cents,price_cents,margin_cents,type,balance_after,created_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			l.ID, l.TenantID, l.UserID, l.RequestID, l.Model, l.InputTokens, l.OutputTokens,
			l.CostCents, l.PriceCents, l.MarginCents, l.Type, l.BalanceAfter, l.CreatedAt); err != nil {
			return 0, 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return fromBal, toBal, nil
}

func (s *Store) InsertUsage(ctx context.Context, u *model.UsageRecord) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO usage_records(id,tenant_id,user_id,api_key_id,api_key_name,request_id,model,provider,channel_id,input_tokens,output_tokens,latency_ms,price_cents,cost_cents,status,error_message,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		u.ID, u.TenantID, u.UserID, u.APIKeyID, u.APIKeyName, u.RequestID, u.Model, u.Provider, u.ChannelID,
		u.InputTokens, u.OutputTokens, u.LatencyMs, u.PriceCents, u.CostCents, u.Status, u.ErrorMessage, u.CreatedAt)
	return err
}

// UsageAggregate 按日/模型聚合用量与费用。
type UsageAggregate struct {
	Bucket       string `json:"bucket"`       // YYYY-MM-DD
	Model        string `json:"model"`
	Requests     int    `json:"requests"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	PriceCents   int64  `json:"price_cents"`
}

// UsageByDay 按天聚合(用户维度)。只统计 usage 账目——账本含 recharge/refund/transfer/adjust
// 等非营收类型(price 正负不一),不过滤会把充值/退款混入"费用"导致数值失真甚至为负。
func (s *Store) UsageByDay(ctx context.Context, userID string, days int) ([]UsageAggregate, error) {
	q := `SELECT to_char(created_at,'YYYY-MM-DD') AS d, model,
	         COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(price_cents),0)
	      FROM billing_ledger
	      WHERE user_id=$1 AND type='usage' AND created_at >= now() - ($2 || ' days')::interval
	      GROUP BY d, model ORDER BY d, model`
	rows, err := s.Pool.Query(ctx, q, userID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageAggregate
	for rows.Next() {
		var a UsageAggregate
		if err := rows.Scan(&a.Bucket, &a.Model, &a.Requests, &a.InputTokens, &a.OutputTokens, &a.PriceCents); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LedgerRecent 最近账目。
func (s *Store) LedgerRecent(ctx context.Context, userID string, limit int) ([]*model.BillingLedger, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cost_cents,price_cents,margin_cents,type,balance_after,created_at
		 FROM billing_ledger WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.BillingLedger
	for rows.Next() {
		l := &model.BillingLedger{}
		if err := rows.Scan(&l.ID, &l.TenantID, &l.UserID, &l.RequestID, &l.Model, &l.InputTokens, &l.OutputTokens,
			&l.CostCents, &l.PriceCents, &l.MarginCents, &l.Type, &l.BalanceAfter, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AdminLedgerRecent 全局最近账目(管理端)。
func (s *Store) AdminLedgerRecent(ctx context.Context, limit int) ([]*model.BillingLedger, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cost_cents,price_cents,margin_cents,type,balance_after,created_at
		 FROM billing_ledger ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.BillingLedger
	for rows.Next() {
		l := &model.BillingLedger{}
		if err := rows.Scan(&l.ID, &l.TenantID, &l.UserID, &l.RequestID, &l.Model, &l.InputTokens, &l.OutputTokens,
			&l.CostCents, &l.PriceCents, &l.MarginCents, &l.Type, &l.BalanceAfter, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AggParams 多维聚合参数。
type AggParams struct {
	Scope   string // "user" | "tenant" | "all"
	ScopeID string // user_id 或 tenant_id(all 忽略)
	GroupBy string // model | provider | api_key
	Bucket  string // minute | hour | day
	From    time.Time
	To      time.Time
}

// AggRow 一行聚合结果。
type AggRow struct {
	Bucket       string `json:"bucket"`
	Dim          string `json:"dim"`
	Requests     int    `json:"requests"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	PriceCents   int64  `json:"price_cents"`
}

func dimColumn(groupBy string) string {
	switch groupBy {
	case "provider":
		return "provider"
	case "api_key":
		return "coalesce(nullif(api_key_name,''), api_key_id)"
	default:
		return "model"
	}
}

func bucketExpr(bucket string) string {
	switch bucket {
	case "minute":
		return "to_char(created_at,'YYYY-MM-DD HH24:MI')"
	case "hour":
		return "to_char(created_at,'YYYY-MM-DD HH24:00')"
	default:
		return "to_char(created_at,'YYYY-MM-DD')"
	}
}

// Aggregate 多维用量聚合(用于按 key/模型/供应商 的 TPM/RPM/费用查询)。
func (s *Store) Aggregate(ctx context.Context, p AggParams) ([]AggRow, error) {
	if p.To.IsZero() {
		p.To = time.Now()
	}
	if p.From.IsZero() {
		p.From = p.To.AddDate(0, 0, -30)
	}
	dim := dimColumn(p.GroupBy)
	bkt := bucketExpr(p.Bucket)
	q := `SELECT ` + bkt + ` AS b, ` + dim + ` AS d, COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), COALESCE(SUM(price_cents),0)
	      FROM usage_records WHERE created_at >= $1 AND created_at < $2 `
	args := []any{p.From, p.To}
	if p.Scope == "user" && p.ScopeID != "" {
		q += ` AND user_id = $3 `
		args = append(args, p.ScopeID)
	} else if p.Scope == "tenant" && p.ScopeID != "" {
		q += ` AND tenant_id = $3 `
		args = append(args, p.ScopeID)
	}
	q += ` GROUP BY b, d ORDER BY b, d`
	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AggRow
	for rows.Next() {
		var r AggRow
		if err := rows.Scan(&r.Bucket, &r.Dim, &r.Requests, &r.InputTokens, &r.OutputTokens, &r.PriceCents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminStats 管理端仪表盘统计。
type AdminStats struct {
	TotalRequests int   `json:"total_requests"`
	TotalRevenue  int64 `json:"total_revenue"`
	TotalCost     int64 `json:"total_cost"`
	ActiveTenants int   `json:"active_tenants"`
	ActiveUsers   int   `json:"active_users"`
}

func (s *Store) AdminStats(ctx context.Context) (AdminStats, error) {
	var st AdminStats
	q := `SELECT
	    (SELECT COUNT(*) FROM usage_records),
	    COALESCE((SELECT SUM(price_cents) FROM billing_ledger WHERE type='usage'),0),
	    COALESCE((SELECT SUM(cost_cents) FROM billing_ledger WHERE type='usage'),0),
	    (SELECT COUNT(*) FROM tenants WHERE status='active'),
	    (SELECT COUNT(*) FROM users WHERE status='active')`
	err := s.Pool.QueryRow(ctx, q).Scan(&st.TotalRequests, &st.TotalRevenue, &st.TotalCost, &st.ActiveTenants, &st.ActiveUsers)
	return st, err
}
