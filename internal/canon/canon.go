// Package canon 定义网关内部的规范消息格式(基于 OpenAI Chat Completions)。
// 入口侧(OpenAI / Anthropic)统一转换为此格式，出口侧 adapter 再转换为各供应商原生格式。
package canon

import "strings"

// Role 角色枚举。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ContentPart 是多模态内容块(OpenAI content parts)。
type ContentPart struct {
	Type       string      `json:"type"` // text | image_url | input_audio | file
	Text       string      `json:"text,omitempty"`
	ImageURL   *ImageURL   `json:"image_url,omitempty"`
	InputAudio *InputAudio `json:"input_audio,omitempty"`
	File       *FileRef    `json:"file,omitempty"`
}

// ImageURL 图片引用。
type ImageURL struct {
	URL    string `json:"url"`              // http(s) | data:base64 | file:<id>
	Detail string `json:"detail,omitempty"` // auto | low | high
}

// InputAudio 音频输入(OpenAI input_audio 格式)。
type InputAudio struct {
	Data   string `json:"data"`   // base64
	Format string `json:"format"` // mp3 | wav ...
}

// FileRef 文件引用(网关扩展,用于文档类输入)。
type FileRef struct {
	FileID   string `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	URL      string `json:"url,omitempty"`
}

// Message 一条消息。Content 为字符串(纯文本)或 ContentPart 数组(多模态)。
type Message struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`            // string | []ContentPart
	ReasoningContent string     `json:"reasoning_content,omitempty"` // 推理模型思考过程(DeepSeek/GLM/Qwen 等),上游透传
	Name             string     `json:"name,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

// ToolCall 工具调用(基本透传)。
type ToolCall struct {
	Index int    `json:"index,omitempty"` // OpenAI 流式增量顺序(非流式可为 0/省略)
	ID    string `json:"id"`
	Type  string `json:"type"` // function
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Tool 工具定义(基本透传)。
type Tool struct {
	Type     string `json:"type"` // function
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters,omitempty"`
	} `json:"function"`
}

// Request 规范化的 Chat 请求(OpenAI 格式)。
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	// StreamOptions 流式选项。网关在流式路径强制 IncludeUsage=true,确保上游返回 usage 帧,
	// 否则 OpenAI 兼容上游默认不带 usage → 流式请求 0 token 计费(静默漏计)。
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  any       `json:"tool_choice,omitempty"`
	User        string    `json:"user,omitempty"`
	// 入口标记,用于 egress 决定响应格式。
	Source string `json:"-"`
}

// StreamOptions OpenAI 流式选项。
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// Usage token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// 缓存命中(读取)token: 上游已缓存的 prompt 片段,通常按折扣计价(OpenAI 0.5x、Anthropic/DeepSeek 0.1x)。
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`
	// 缓存写入token: 本次写入缓存的 prompt 片段(Anthropic 概念),通常按加价计价(1.25x)。
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

// Response 非流式规范响应(OpenAI 格式)。
type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"` // chat.completion
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 一个候选。
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"` // stop | length | tool_calls
}

// StreamChunk 一帧流式响应(OpenAI chunk 结构)。
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"` // chat.completion.chunk
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"` // 最后一帧可能带 usage
	// StreamError 上游流式出错(网络断流/读超时/行超长)的内部信号,json:"-" 不序列化给客户端。
	// 由 provider adapter 在 Scanner 结束后检查 sc.Err() 填入;relay 据此触发渠道熔断,
	// 避免上游 200 OK 后中途截断的病态渠道被当作健康而持续漏流量。
	StreamError string `json:"-"`
}

// StreamChoice 流式候选。
type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Message `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// TextContent 便捷提取消息文本内容。
func TextContent(m Message) string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []ContentPart:
		var sb strings.Builder
		for _, p := range v {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	case []interface{}:
		var sb strings.Builder
		for _, it := range v {
			if mp, ok := it.(map[string]any); ok {
				if t, _ := mp["type"].(string); t == "text" {
					if s, ok := mp["text"].(string); ok {
						sb.WriteString(s)
					}
				}
			}
		}
		return sb.String()
	}
	return ""
}

// AsParts 将 Message.Content 统一为 ContentPart 切片。
// 支持三种入参形态:强类型 []ContentPart(内部构造)、string(纯文本)、
// []interface{}(JSON 反序列化的 OpenAI content parts 数组——入口用 ShouldBindJSON
// 绑定到 canon.Request 时,Content any 会得到此形态)。后者必须在此归一,
// 否则跨协议多模态(image_url/audio/file)会在出口适配器全部丢失。
func AsParts(m Message) []ContentPart {
	switch v := m.Content.(type) {
	case []ContentPart:
		return v
	case string:
		return []ContentPart{{Type: "text", Text: v}}
	case []interface{}:
		out := make([]ContentPart, 0, len(v))
		for _, it := range v {
			mp, ok := it.(map[string]any)
			if !ok {
				continue
			}
			p := ContentPart{}
			if t, ok := mp["type"].(string); ok {
				p.Type = t
			}
			if s, ok := mp["text"].(string); ok {
				p.Text = s
			}
			if iu, ok := mp["image_url"].(map[string]any); ok {
				p.ImageURL = &ImageURL{}
				if u, ok := iu["url"].(string); ok {
					p.ImageURL.URL = u
				}
				if d, ok := iu["detail"].(string); ok {
					p.ImageURL.Detail = d
				}
			}
			if ia, ok := mp["input_audio"].(map[string]any); ok {
				p.InputAudio = &InputAudio{}
				if d, ok := ia["data"].(string); ok {
					p.InputAudio.Data = d
				}
				if f, ok := ia["format"].(string); ok {
					p.InputAudio.Format = f
				}
			}
			out = append(out, p)
		}
		return out
	}
	return nil
}
