# 渠道模型独立实体化设计（channel_models）

> 状态：已实现（migration `0021_channel_models`,数据迁移 26→26,2026-07)· 原设计 2026-07-18
> 范围：把当前内嵌于 `channels` 表的「模型清单 / 模型映射 / 按模型成本」三组 JSON/数组字段，规范化为独立实体表 `channel_models`，使每个「渠道 × 模型」可独立配置成本、上游名、状态、权重。

## 1. 背景与动机

当前 `channels` 表把「渠道」与「渠道下的模型配置」混在一张表：

```
channels(
  id, provider, name, base_url, api_key_enc, priority, weight,  -- 渠道级
  tenant_id, status, created_at,
  models text[],            -- 内嵌:该渠道支持哪些模型
  model_mappings jsonb,     -- 内嵌:logical=upstream 名映射
  input_cost_cents_per_m, output_cost_cents_per_m,  -- 渠道级默认成本
  model_costs jsonb         -- 内嵌:按模型覆盖成本(输入/输出/缓存读/缓存写)
)
```

这套设计对标 One-API/New-API，**功能上够用**，但有结构性限制：

1. **模型无法独立启停**：要禁用某渠道下的单个模型（如 qwen-turbo 上游故障），只能从 `models[]` 删除，丢失其成本配置；或禁用整个渠道，牵连同渠道其他模型。
2. **模型无法独立权重**：同一渠道内所有模型共享 `priority/weight`，无法让 qwen-max 走高权重、qwen-turbo 走低权重。
3. **成本配置是 JSON 黑盒**：`model_costs` 是非结构化 jsonb，无法建索引、无法独立查询/聚合（如「qwen3-max 在所有渠道的成本对比」要全表扫 + JSON 解析）。
4. **每模型差异化无干净落点**：未来若要给单个渠道×模型加独立配额/限流/标签，继续塞 JSON 会越来越乱。

规范化为 `channel_models` 后，每个「渠道 × 模型」是一等实体行，上述能力自然落地。

## 2. 范围边界

**本次做**：
- 新建 `channel_models` 表（成本 / 上游名 / 权重 / 状态）。
- 数据迁移：把现有 `channels.models[] + model_mappings + model_costs` 展开灌入新表。
- 全链路改造：migration / model / store / relay route / admin API / 前端 Channels.vue。

**本次不做**（明确剥离，避免范围蔓延）：
- **每模型级 rpm/tpm 限流**：这是 **API Key 维度**（key 调某模型的速率），不是渠道维度。属 `api_keys` 的 `model_limits jsonb` 工作，与 `channel_models` 正交，不在本次。若后续需要，单独立项。
- 渠道×模型的独立熔断器维度：当前熔断按 `channel_id`，本次不改（保持按渠道熔断；模型级熔断待真实需求出现再做）。
- 向后兼容旧字段：迁移后**移除** `channels.models/model_mappings/model_costs`（一次性切换，不留双写包袱）。

## 3. 数据模型

### 3.1 `channel_models` 表

```sql
CREATE TABLE channel_models (
  id            text        PRIMARY KEY,
  channel_id    text        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  model_name    text        NOT NULL,              -- 逻辑模型名(对外暴露)
  upstream_model text       NOT NULL DEFAULT '',   -- 上游真实模型名(''=同名直通,取代 model_mappings)
  input_cost_cents_per_m   bigint NOT NULL DEFAULT 0,
  output_cost_cents_per_m  bigint NOT NULL DEFAULT 0,
  cache_read_cost_cents_per_m  bigint NOT NULL DEFAULT 0,
  cache_write_cost_cents_per_m bigint NOT NULL DEFAULT 0,
  weight        int         NOT NULL DEFAULT 1,    -- 该模型在此渠道的权重(0/缺省=继承渠道 weight)
  status        text        NOT NULL DEFAULT 'active',  -- active | disabled:单模型级启停
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE(channel_id, model_name)
);
CREATE INDEX idx_channel_models_lookup  ON channel_models(model_name, status);
CREATE INDEX idx_channel_models_channel ON channel_models(channel_id);
```

**设计要点**：
- `UNIQUE(channel_id, model_name)`：同一渠道同模型仅一行（防重复挂载）。
- `upstream_model` 取代 `model_mappings`：从「map 查找」变为「行字段」，可直接索引、可空（空=同名直通）。
- `weight` 字段：0/缺省表示「继承渠道级 weight」（避免强制每模型都填）；非 0 时覆盖。
- `status`：单模型级启停——禁用某模型不动渠道其他模型。
- 成本四项直接是列：可索引、可聚合（如 `SELECT AVG(input_cost_cents_per_m) FROM channel_models WHERE model_name='qwen3-max'`）。

### 3.2 `channels` 表精简

迁移后 channels 仅保留渠道级属性：

```
channels(
  id, tenant_id, provider, name, base_url, api_key_enc,
  priority, weight,        -- 渠道级默认(priority 影响该渠道所有模型的选路顺序)
  input_cost_cents_per_m, output_cost_cents_per_m,  -- 渠道级默认成本(channel_models 成本为 0 时回退)
  status, created_at
)
-- 移除: models, model_mappings, model_costs
```

**渠道级成本保留的理由**：`channel_models` 的成本若全 0，回退到渠道级默认（减少重复填写，如某渠道所有模型成本相近时只填渠道级）。

### 3.3 Go model

```go
// ChannelModel 一个渠道×模型的独立配置(取代内嵌的 models[]/model_mappings/model_costs)。
type ChannelModel struct {
    ID                      string `json:"id"`
    ChannelID               string `json:"channel_id"`
    ModelName               string `json:"model_name"`
    UpstreamModel           string `json:"upstream_model"`           // 空=同名直通
    InputCostCentsPerM      int64  `json:"input_cost_cents_per_m"`
    OutputCostCentsPerM     int64  `json:"output_cost_cents_per_m"`
    CacheReadCostCentsPerM  int64  `json:"cache_read_cost_cents_per_m"`
    CacheWriteCostCentsPerM int64  `json:"cache_write_cost_cents_per_m"`
    Weight                  int    `json:"weight"`                   // 0=继承渠道 weight
    Status                  string `json:"status"`                   // active | disabled
    CreatedAt               time.Time `json:"created_at"`
}
```

`model.Channel` 移除 `Models/ModelMappings/ModelCosts` 三字段，新增 `ChannelModels []ChannelModel`（编辑/列表时联表带回）。

## 4. 迁移（0021）

分两步：建表 + 数据搬迁。搬迁用 **Go 迁移代码**（而非纯 SQL），因为要合并 `models[]` 展开 + `model_mappings`/`model_costs` 查找，逻辑比纯 SQL jsonb 展开清晰且可测。

```go
// 0021 迁移伪代码:
// 1. CREATE TABLE channel_models ... + 索引(如上)
// 2. 遍历 channels 每一行:
//      for modelName in models[]:
//        upstream = model_mappings[modelName] || ""   // 无映射=同名
//        cost     = model_costs[modelName]           // 无=全 0(回退渠道级)
//        INSERT channel_models(channel_id, model_name, upstream_model, cost..., weight=1, status='active')
// 3. ALTER TABLE channels DROP COLUMN models, model_mappings, model_costs
```

down 迁移：重建三列 + 反向序列化（GROUP BY channel_id 聚合 channel_models 回 jsonb/array）+ DROP channel_models。

**风险控制**：迁移在事务内执行；搬迁失败整体回滚，不留半迁移状态。

## 5. 各层改造清单

### 5.1 store（`store_channels.go`）

- `ChannelsForModel`：从 `ANY(models)` 过滤改为 **JOIN channel_models**：
  ```sql
  SELECT c.*, cm.id AS cm_id, cm.upstream_model, cm.input_cost_cents_per_m AS cm_in, ...
  FROM channels c
  JOIN channel_models cm ON cm.channel_id = c.id
  WHERE c.status='active' AND cm.status='active'
    AND (c.tenant_id IS NULL OR c.tenant_id=$1)
    AND cm.model_name = $2
  ORDER BY (c.tenant_id IS NOT NULL) DESC, c.priority DESC, c.weight DESC
  ```
  返回 `[]*ChannelWithModel`（channel + 该模型配置），取代 `[]*Channel`。
- `CreateChannel` / `UpdateChannel`：改为**事务**——写 channels 行 + 批量 upsert/delete channel_models（前端提交完整模型列表，diff 后写）。UpdateChannelRouting 同理。
- 新增 `ChannelModels` CRUD：`ListChannelModels(channelID)`、`UpsertChannelModel`、`DeleteChannelModel`、`SetChannelModelStatus`（单模型启停）。
- `scanChannel`：移除 mappings/costs 反序列化；`GetChannel/ListChannels` 联表带回 `ChannelModels`（列表页展示用）。

### 5.2 relay route（`relay.go:77 route`）

`route` 当前对每个 channel 取 `ModelMappings[model]`（upstream）和 `CostForAll(model)`（model_costs）。改造后这些直接来自 JOIN 回的 `ChannelModel` 行：

```go
// resolvedChannel 字段调整: upstream/costs 直接取自 ChannelModel(不再查 map)
out = append(out, resolvedChannel{
    ch: &provider.Channel{ID: c.ID, Provider: pv, BaseURL: c.BaseURL, APIKey: keyPlain},
    upstream:   cm.UpstreamModel,  // 若空则用 modelName
    inputCost:  costOr(cm.InputCostCentsPerM, c.InputCostCentsPerM),  // 模型级 0→渠道级回退
    outputCost: costOr(cm.OutputCostCentsPerM, c.OutputCostCentsPerM),
    cacheReadCost:  cm.CacheReadCostCentsPerM,
    cacheWriteCost: cm.CacheWriteCostCentsPerM,
})
```

`Channel.CostForAll` / `CostFor` 方法删除（逻辑移入 route 的回退）。`ModelMappings` 字段引用全清。

### 5.3 admin API（`admin.go`）

- `channelReq` 结构：`Models []string / ModelMappings / ModelCosts` → `ChannelModels []channelModelReq`（每项 `{model_name, upstream_model, in, out, cache_read, cache_write, weight, status}`）。
- `adminCreateChannel/adminUpdateChannel`：调用新的事务型 store 方法。
- `adminListChannels` 输出：`model_costs/model_mappings/models` → `channel_models` 数组。
- `adminUpdateChannelRouting`：改为操作 channel_models（调单模型启停/权重/成本）。
- 新增 `PATCH /api/admin/channels/:id/models/:model/status`：单模型启停（无需动整个渠道）。

### 5.4 前端 Channels.vue

**几乎不变**——当前「模型与成本」动态表格天然就是 channel_models 的形态。只需：
- 提交 payload：`modelRows` → `channel_models[]`（每行加 `upstream_model`/`weight`/`status` 字段）。
- 编辑回填：从 `channel.channel_models[]` 直接映射到 `modelRows`（比现在从 `models[]+model_costs{}` 两个源拼更简单）。
- 表格补两列：**上游模型名**（选填）、**启用**开关（单模型启停）。
- 渠道列表的「模型」列：显示 `channel_models.length` + 启用数。

## 6. 路由回退语义（保持不变）

`Channel.CostForAll` 当前的「model_costs[model] 优先，回退渠道级默认」语义，改造后由 route 内 `costOr(model级, 渠道级)` 显式实现，**行为完全一致**——这是迁移的正确性锚点（计费金额迁移前后必须不变）。

## 7. 验证计划

1. **迁移正确性**：迁移前后，对同一 (channel, model) 跑 `route`，断言 `upstream/inputCost/outputCost/cacheReadCost/cacheWriteCost` 五元组完全相等（写单测 `TestMigrationRouteEquivalence`）。
2. **端到端**：
   - 计费回归：发 chat 请求，price/cost 与迁移前一致。
   - 单模型启停：禁用某渠道的 qwen-turbo → 该模型请求故障转移到其他渠道；同渠道 qwen-max 不受影响。
   - 上游名映射：`channel_models.upstream_model` 设 `qwen3-max=qwen-max-latest` → 上游收到正确模型名。
   - 成本聚合查询：`SELECT ... FROM channel_models WHERE model_name=X` 能直接对比跨渠道成本。
3. **回归**：R8-R12 计费审计单测全绿；tool use / 请求日志 / 配额三大能力不受影响。

## 8. 影响面与工作量评估

| 层 | 文件 | 改动量 |
|---|---|---|
| DB | migration 0021（含数据搬迁 Go 代码） | 中 |
| model | `model/models.go`（Channel 精简 + ChannelModel 新增） | 小 |
| store | `store_channels.go`（6 方法重写 + JOIN） | 中-大 |
| relay | `relay.go` route（resolvedChannel + 回退） | 小 |
| admin | `admin.go`（3 handler + 新增单模型启停） | 中 |
| 前端 | `Channels.vue`（payload/回填 + 2 列） | 小 |
| 测试 | 迁移等价性单测 + 现有测试适配 | 中 |

**总体**：中等工作量，集中在 store 层（事务化 + JOIN）和迁移（数据搬迁正确性）。前端反而是最省的（表格形态已对齐）。

## 9. 风险与回滚

- **风险 1：数据搬迁错漏**（如 model_costs key 与 models[] 不一致）。缓解：迁移后断言 `COUNT(channel_models) == SUM(array_length(models,1))`；写等价性单测。
- **风险 2：route 热路径 JOIN 性能**。缓解：`idx_channel_models_lookup(model_name, status)` 覆盖主查询；route 按 model_name 查，命中索引。
- **回滚**：down 迁移重建三列 + 反向聚合 + DROP 新表。因迁移在事务内，失败即回滚。

## 10. 待评审决策点

1. **`weight` 语义**：channel_models.weight 设「0=继承渠道 weight」还是「强制每模型填」？→ 建议继承（减少填写）。
2. **渠道级成本是否保留**？→ 建议保留（作为 model 级 0 的回退，减少重复填写）。
3. **单模型启停 API** 是否本期就做？→ 建议做（这是规范化的核心收益之一，否则白拆）。
4. **模型级 rpm/tpm** 确认剥离到独立项目？→ 是（key 维度，非渠道维度）。

---

**评审通过后**，按 writing-plans 出实施计划，分批落地（migration+store → relay → admin → 前端 → 测试），每批 build/test/lint 绿后进入下一批。
