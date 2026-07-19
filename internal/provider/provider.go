// Package provider 定义供应商适配端口(Provider 接口)与运行时上下文。
package provider

import (
	"context"
	"sort"

	"github.com/aitoys/llm-gateway/internal/canon"
)

// Provider 供应商适配端口。每个供应商(bailian/volcark/qianfan/mock)实现此接口。
type Provider interface {
	// Name 供应商标识。
	Name() string
	// Chat 非流式对话。
	Chat(ctx context.Context, ch *Channel, req *canon.Request) (*canon.Response, error)
	// ChatStream 流式对话。返回的 channel: 每帧 *canon.StreamChunk,流结束或出错时关闭,
	// 出错通过 Err 字段返回。
	ChatStream(ctx context.Context, ch *Channel, req *canon.Request) (<-chan *canon.StreamChunk, error)
	// Embeddings 文本向量(本迭代可返回 ErrNotSupported)。
	Embeddings(ctx context.Context, ch *Channel, input []string, model string) ([][]float32, *canon.Usage, error)
}

// Channel 是一次调用的渠道运行时上下文(已解密)。
type Channel struct {
	ID       string
	TenantID string // 空表示平台默认
	Provider string
	BaseURL  string
	APIKey   string // 已解密明文
	Extra    map[string]any
}

// Registry 供应商注册表。
type Registry struct {
	providers map[string]Provider
}

func NewRegistry() *Registry { return &Registry{providers: make(map[string]Provider)} }

// Register 注册一个供应商。
func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
}

// Get 取供应商; 不存在返回 nil。
func (r *Registry) Get(name string) Provider {
	return r.providers[name]
}

// Names 返回已注册供应商标识(稳定排序),供 API 暴露给前端做单一数据源。
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.providers))
	for k := range r.providers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
