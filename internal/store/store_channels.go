package store

import (
	"context"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// channelCols channels 表列(规范化后:模型配置独立到 channel_models 表,不再内嵌)。
const channelCols = "id,tenant_id,provider,name,base_url,api_key_enc,priority,weight,input_cost_cents_per_m,output_cost_cents_per_m,status,created_at"

// channelColsAliased 带 c. 前缀的列(供 JOIN channel_models 时避免与 cm.id 等列名歧义)。
const channelColsAliased = "c.id,c.tenant_id,c.provider,c.name,c.base_url,c.api_key_enc,c.priority,c.weight,c.input_cost_cents_per_m,c.output_cost_cents_per_m,c.status,c.created_at"

func scanChannel(scan func(...any) error, c *model.Channel) error {
	return scan(&c.ID, &c.TenantID, &c.Provider, &c.Name, &c.BaseURL, &c.APIKeyEnc,
		&c.Priority, &c.Weight, &c.InputCostCentsPerM, &c.OutputCostCentsPerM, &c.Status, &c.CreatedAt)
}

// CreateChannel 事务创建渠道 + 其下所有模型配置(channel_models)。
// 调用方在 model.Channel.ChannelModels 提供完整模型清单;全量写入。
func (s *Store) CreateChannel(ctx context.Context, c *model.Channel) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO channels(id,tenant_id,provider,name,base_url,api_key_enc,priority,weight,input_cost_cents_per_m,output_cost_cents_per_m,status,created_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			c.ID, c.TenantID, c.Provider, c.Name, c.BaseURL, c.APIKeyEnc,
			c.Priority, c.Weight, c.InputCostCentsPerM, c.OutputCostCentsPerM, c.Status, c.CreatedAt); err != nil {
			return err
		}
		return insertChannelModels(ctx, tx, c.ID, c.ChannelModels)
	})
}

// UpdateChannel 事务全量更新渠道可变字段 + 模型清单(模型清单整体替换:删旧插新)。
// id/created_at/tenant_id 不变。密钥密文由调用方决定是否更新(空=保留旧密钥需调用方先取回)。
func (s *Store) UpdateChannel(ctx context.Context, c *model.Channel) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx,
			`UPDATE channels SET
			   provider=$2,name=$3,base_url=$4,api_key_enc=$5,
			   priority=$6,weight=$7,input_cost_cents_per_m=$8,output_cost_cents_per_m=$9,status=$10
			 WHERE id=$1`,
			c.ID, c.Provider, c.Name, c.BaseURL, c.APIKeyEnc,
			c.Priority, c.Weight, c.InputCostCentsPerM, c.OutputCostCentsPerM, c.Status)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNotFound
		}
		// 模型清单整体替换(全量覆盖;前端提交完整列表)。
		if _, err := tx.Exec(ctx, `DELETE FROM channel_models WHERE channel_id=$1`, c.ID); err != nil {
			return err
		}
		return insertChannelModels(ctx, tx, c.ID, c.ChannelModels)
	})
}

// insertChannelModels 批量写入某渠道的模型配置(空列表跳过)。
func insertChannelModels(ctx context.Context, tx pgx.Tx, channelID string, cms []model.ChannelModel) error {
	for _, cm := range cms {
		if cm.ModelName == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO channel_models(id,channel_id,model_name,upstream_model,input_cost_cents_per_m,output_cost_cents_per_m,cache_read_cost_cents_per_m,cache_write_cost_cents_per_m,weight,status,created_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			cm.ID, channelID, cm.ModelName, cm.UpstreamModel,
			cm.InputCostCentsPerM, cm.OutputCostCentsPerM, cm.CacheReadCostCentsPerM, cm.CacheWriteCostCentsPerM,
			cm.Weight, cm.Status, cm.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

// ChannelsForModel 取某租户某模型可用的渠道 + 该模型在该渠道的配置(租户 BYOK 优先,其次平台默认)。
// JOIN channel_models: 每行 = 一个渠道 + 该 (渠道,模型) 的配置;返回的 Channel.ChannelModels 仅含该模型一项(route 直接取)。
func (s *Store) ChannelsForModel(ctx context.Context, tenantID, modelName string) ([]*model.Channel, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+channelColsAliased+`,
		        cm.id,cm.channel_id,cm.model_name,cm.upstream_model,cm.input_cost_cents_per_m,cm.output_cost_cents_per_m,cm.cache_read_cost_cents_per_m,cm.cache_write_cost_cents_per_m,cm.weight,cm.status,cm.created_at
		 FROM channels c JOIN channel_models cm ON cm.channel_id=c.id
		 WHERE c.status='active' AND cm.status='active'
		   AND (c.tenant_id IS NULL OR c.tenant_id=$1)
		   AND cm.model_name=$2
		 ORDER BY (c.tenant_id IS NOT NULL) DESC, c.priority DESC, c.weight DESC, c.id`, tenantID, modelName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Channel
	for rows.Next() {
		c := &model.Channel{}
		cm := &model.ChannelModel{}
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Provider, &c.Name, &c.BaseURL, &c.APIKeyEnc,
			&c.Priority, &c.Weight, &c.InputCostCentsPerM, &c.OutputCostCentsPerM, &c.Status, &c.CreatedAt,
			&cm.ID, &cm.ChannelID, &cm.ModelName, &cm.UpstreamModel,
			&cm.InputCostCentsPerM, &cm.OutputCostCentsPerM, &cm.CacheReadCostCentsPerM, &cm.CacheWriteCostCentsPerM,
			&cm.Weight, &cm.Status, &cm.CreatedAt); err != nil {
			return nil, err
		}
		c.ChannelModels = []model.ChannelModel{*cm}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, id string) (*model.Channel, error) {
	c := &model.Channel{}
	row := s.Pool.QueryRow(ctx, `SELECT `+channelCols+` FROM channels WHERE id=$1`, id)
	if err := scanChannel(row.Scan, c); err != nil {
		return nil, err
	}
	cms, err := s.ListChannelModels(ctx, id)
	if err != nil {
		return nil, err
	}
	c.ChannelModels = cms
	return c, nil
}

// ListChannels 列出渠道(含各自模型清单)。tenantFilter 空→全部(平台管理员);
// 非空→该租户 BYOK + 平台默认。可见范围与 ChannelsForModel 一致。
func (s *Store) ListChannels(ctx context.Context, tenantFilter string) ([]*model.Channel, error) {
	var rows pgx.Rows
	var err error
	if tenantFilter == "" {
		rows, err = s.Pool.Query(ctx, `SELECT `+channelCols+` FROM channels ORDER BY (tenant_id IS NULL) DESC, created_at`)
	} else {
		rows, err = s.Pool.Query(ctx,
			`SELECT `+channelCols+` FROM channels WHERE tenant_id IS NULL OR tenant_id=$1 ORDER BY (tenant_id IS NULL) DESC, created_at`,
			tenantFilter)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Channel
	for rows.Next() {
		c := &model.Channel{}
		if err := scanChannel(rows.Scan, c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 批量带出每个渠道的模型清单(避免 N+1: 一次查全部渠道的 channel_models 再分组)。
	if len(out) > 0 {
		ids := make([]string, 0, len(out))
		for _, c := range out {
			ids = append(ids, c.ID)
		}
		all, err := s.channelModelsByChannels(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, c := range out {
			c.ChannelModels = all[c.ID]
		}
	}
	return out, nil
}

func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM channels WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetChannelStatus(ctx context.Context, id, status string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE channels SET status=$2 WHERE id=$1`, id, status)
	return err
}

// SetChannelModelStatus 单模型级启停(禁用某渠道的单个模型,不影响同渠道其他模型)。
func (s *Store) SetChannelModelStatus(ctx context.Context, channelID, modelName, status string) error {
	ct, err := s.Pool.Exec(ctx, `UPDATE channel_models SET status=$3 WHERE channel_id=$1 AND model_name=$2`,
		channelID, modelName, status)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddChannelModel 挂载单个模型到渠道(默认成本 0=回退渠道级,active)。
// 已存在则返回 PG unique violation(23505);调用方用 isUniqueViolation 识别做幂等处理(无 ErrFound sentinel)。
func (s *Store) AddChannelModel(ctx context.Context, channelID, modelName string) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO channel_models(id,channel_id,model_name,upstream_model,input_cost_cents_per_m,output_cost_cents_per_m,cache_read_cost_cents_per_m,cache_write_cost_cents_per_m,weight,status,created_at)
		 VALUES($1,$2,$3,'',0,0,0,0,0,'active',now())`, storeID(), channelID, modelName)
	return err
}

// DeleteChannelModel 从渠道移除单个模型挂载。
func (s *Store) DeleteChannelModel(ctx context.Context, channelID, modelName string) error {
	ct, err := s.Pool.Exec(ctx, `DELETE FROM channel_models WHERE channel_id=$1 AND model_name=$2`, channelID, modelName)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateChannelRouting 仅更新渠道级路由字段(优先级/权重/渠道级默认成本)。
// 模型清单的增删改由 UpdateChannel(全量)或专用 ChannelModel CRUD 承担。
func (s *Store) UpdateChannelRouting(ctx context.Context, id string, priority, weight int, inCost, outCost int64) error {
	ct, err := s.Pool.Exec(ctx,
		`UPDATE channels SET priority=$2, weight=$3, input_cost_cents_per_m=$4, output_cost_cents_per_m=$5 WHERE id=$1`,
		id, priority, weight, inCost, outCost)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListChannelModels 取某渠道的全部模型配置(按模型名排序)。
func (s *Store) ListChannelModels(ctx context.Context, channelID string) ([]model.ChannelModel, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id,channel_id,model_name,upstream_model,input_cost_cents_per_m,output_cost_cents_per_m,cache_read_cost_cents_per_m,cache_write_cost_cents_per_m,weight,status,created_at
		 FROM channel_models WHERE channel_id=$1 ORDER BY model_name`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ChannelModel
	for rows.Next() {
		var cm model.ChannelModel
		if err := rows.Scan(&cm.ID, &cm.ChannelID, &cm.ModelName, &cm.UpstreamModel,
			&cm.InputCostCentsPerM, &cm.OutputCostCentsPerM, &cm.CacheReadCostCentsPerM, &cm.CacheWriteCostCentsPerM,
			&cm.Weight, &cm.Status, &cm.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, cm)
	}
	return out, rows.Err()
}

// channelModelsByChannels 批量取多个渠道的模型配置,按 channel_id 分组返回(避免 N+1)。
func (s *Store) channelModelsByChannels(ctx context.Context, channelIDs []string) (map[string][]model.ChannelModel, error) {
	out := map[string][]model.ChannelModel{}
	if len(channelIDs) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id,channel_id,model_name,upstream_model,input_cost_cents_per_m,output_cost_cents_per_m,cache_read_cost_cents_per_m,cache_write_cost_cents_per_m,weight,status,created_at
		 FROM channel_models WHERE channel_id = ANY($1) ORDER BY channel_id, model_name`, channelIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cm model.ChannelModel
		if err := rows.Scan(&cm.ID, &cm.ChannelID, &cm.ModelName, &cm.UpstreamModel,
			&cm.InputCostCentsPerM, &cm.OutputCostCentsPerM, &cm.CacheReadCostCentsPerM, &cm.CacheWriteCostCentsPerM,
			&cm.Weight, &cm.Status, &cm.CreatedAt); err != nil {
			return nil, err
		}
		out[cm.ChannelID] = append(out[cm.ChannelID], cm)
	}
	return out, rows.Err()
}

// inTx 在事务中执行 fn,出错自动回滚(含 panic 安全)。
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) (err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx) // 提交后回滚为 no-op;失败时回滚
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
