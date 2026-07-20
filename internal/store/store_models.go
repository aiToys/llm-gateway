package store

import (
	"context"
	"errors"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = errors.New("not found")

const modelCols = "model_name,input_price_cents_per_m,output_price_cents_per_m,cache_read_price_cents_per_m,cache_write_price_cents_per_m,enabled,description,long_desc,tags,capabilities,context_length,routing_strategy,COALESCE(pinned_channel_id,'') AS pinned_channel_id,COALESCE(providers,'{}') AS providers"

// nullableStr 空字符串映射为 SQL NULL,供 nullable 列写入。
func nullableStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) UpsertModel(ctx context.Context, m *model.ModelDef) error {
	strategy := model.NormalizedRoutingStrategy(m.RoutingStrategy)
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO models(model_name,input_price_cents_per_m,output_price_cents_per_m,cache_read_price_cents_per_m,cache_write_price_cents_per_m,enabled,description,long_desc,tags,capabilities,context_length,routing_strategy,pinned_channel_id,providers)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 ON CONFLICT(model_name) DO UPDATE SET
		   input_price_cents_per_m=EXCLUDED.input_price_cents_per_m,
		   output_price_cents_per_m=EXCLUDED.output_price_cents_per_m,
		   cache_read_price_cents_per_m=EXCLUDED.cache_read_price_cents_per_m,
		   cache_write_price_cents_per_m=EXCLUDED.cache_write_price_cents_per_m,
		   enabled=EXCLUDED.enabled,
		   description=EXCLUDED.description,
		   long_desc=EXCLUDED.long_desc,
		   tags=EXCLUDED.tags,
		   capabilities=EXCLUDED.capabilities,
		   context_length=EXCLUDED.context_length,
		   routing_strategy=EXCLUDED.routing_strategy,
		   pinned_channel_id=EXCLUDED.pinned_channel_id,
		   providers=EXCLUDED.providers`,
		m.ModelName, m.InputPriceCentsPerM, m.OutputPriceCentsPerM, m.CacheReadPriceCentsPerM, m.CacheWritePriceCentsPerM, m.Enabled,
		m.Description, m.LongDesc, nonNilStrSlice(m.Tags), nonNilStrSlice(m.Capabilities), m.ContextLength, strategy, nullableStr(m.PinnedChannelID), nonNilStrSlice(m.Providers))
	return err
}

func (s *Store) GetModel(ctx context.Context, name string) (*model.ModelDef, error) {
	m := &model.ModelDef{}
	err := s.Pool.QueryRow(ctx,
		`SELECT `+modelCols+` FROM models WHERE model_name=$1`, name).
		Scan(&m.ModelName, &m.InputPriceCentsPerM, &m.OutputPriceCentsPerM, &m.CacheReadPriceCentsPerM, &m.CacheWritePriceCentsPerM, &m.Enabled,
			&m.Description, &m.LongDesc, &m.Tags, &m.Capabilities, &m.ContextLength, &m.RoutingStrategy, &m.PinnedChannelID, &m.Providers)
	if err != nil {
		// 将 pgx.ErrNoRows 归一为 ErrNotFound 哨兵,与 store 其余部分一致;
		// 否则 relay.route 无法区分"模型不存在"(应 404)与底层 DB 错误(500),误报 upstream_error。
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

func (s *Store) ListModels(ctx context.Context, onlyEnabled bool) ([]*model.ModelDef, error) {
	q := `SELECT ` + modelCols + ` FROM models`
	if onlyEnabled {
		q += ` WHERE enabled=true`
	}
	q += ` ORDER BY model_name`
	rows, err := s.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ModelDef
	for rows.Next() {
		m := &model.ModelDef{}
		if err := rows.Scan(&m.ModelName, &m.InputPriceCentsPerM, &m.OutputPriceCentsPerM, &m.CacheReadPriceCentsPerM, &m.CacheWritePriceCentsPerM, &m.Enabled,
			&m.Description, &m.LongDesc, &m.Tags, &m.Capabilities, &m.ContextLength, &m.RoutingStrategy, &m.PinnedChannelID, &m.Providers); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteModel(ctx context.Context, name string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM models WHERE model_name=$1`, name)
	return err
}

// ListTenantOverrides 取某租户的全部模型覆盖记录(用于"租户启用了哪些模型")。
func (s *Store) ListTenantOverrides(ctx context.Context, tenantID string) ([]*model.TenantModelOverride, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT tenant_id,model_name,input_price_cents_per_m,output_price_cents_per_m,enabled
		 FROM tenant_model_overrides WHERE tenant_id=$1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.TenantModelOverride
	for rows.Next() {
		o := &model.TenantModelOverride{}
		if err := rows.Scan(&o.TenantID, &o.ModelName, &o.InputPriceCentsPerM, &o.OutputPriceCentsPerM, &o.Enabled); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SetTenantModelEnabled 启停租户对某模型的使用。售价首次写入时取平台价,已存在时只改 enabled 不动价。
func (s *Store) SetTenantModelEnabled(ctx context.Context, tenantID, modelName string, enabled bool, fallbackIn, fallbackOut int64) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO tenant_model_overrides(tenant_id,model_name,input_price_cents_per_m,output_price_cents_per_m,enabled)
		 VALUES($1,$2,$3,$4,$5)
		 ON CONFLICT(tenant_id,model_name) DO UPDATE SET enabled=EXCLUDED.enabled`,
		tenantID, modelName, fallbackIn, fallbackOut, enabled)
	return err
}

func (s *Store) UpsertTenantOverride(ctx context.Context, o *model.TenantModelOverride) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO tenant_model_overrides(tenant_id,model_name,input_price_cents_per_m,output_price_cents_per_m,enabled)
		 VALUES($1,$2,$3,$4,$5)
		 ON CONFLICT(tenant_id,model_name) DO UPDATE SET
		   input_price_cents_per_m=EXCLUDED.input_price_cents_per_m,
		   output_price_cents_per_m=EXCLUDED.output_price_cents_per_m,
		   enabled=EXCLUDED.enabled`,
		o.TenantID, o.ModelName, o.InputPriceCentsPerM, o.OutputPriceCentsPerM, o.Enabled)
	return err
}

// EffectivePrice 计算租户对某模型的生效定价: 租户覆盖优先,否则全局。
// 成本不在此决定(归渠道),售价与启停可被租户覆盖。
func (s *Store) EffectivePrice(ctx context.Context, tenantID, modelName string) (*model.ModelDef, error) {
	var in, out int64
	var enabled bool
	err := s.Pool.QueryRow(ctx,
		`SELECT input_price_cents_per_m,output_price_cents_per_m,enabled
		 FROM tenant_model_overrides WHERE tenant_id=$1 AND model_name=$2`,
		tenantID, modelName).Scan(&in, &out, &enabled)
	if err == nil {
		if !enabled {
			return nil, ErrNotFound
		}
		// 策略是模型级属性,租户覆盖定价时仍从全局模型读取。
		// 策略与缓存售价是模型级属性,租户覆盖定价时仍从全局模型读取。
		rs, pin := "", ""
		var cr, cw int64
		if gm, gerr := s.GetModel(ctx, modelName); gerr == nil {
			rs = gm.RoutingStrategy
			pin = gm.PinnedChannelID
			cr = gm.CacheReadPriceCentsPerM
			cw = gm.CacheWritePriceCentsPerM
		}
		return &model.ModelDef{ModelName: modelName, InputPriceCentsPerM: in, OutputPriceCentsPerM: out, CacheReadPriceCentsPerM: cr, CacheWritePriceCentsPerM: cw, Enabled: true, RoutingStrategy: rs, PinnedChannelID: pin}, nil
	}
	// 区分"租户无覆盖(pgx.ErrNoRows → 回退全局)"与"DB 错误(应上抛;否则连接抖动时用户按错误价计费)"。
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	// 回退到全局
	m, gerr := s.GetModel(ctx, modelName)
	if gerr != nil {
		return nil, gerr
	}
	return m, nil
}
