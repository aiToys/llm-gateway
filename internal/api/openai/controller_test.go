package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/relay"
	"github.com/gin-gonic/gin"
)

// TestWriteRelayErrMapping 验证 relay 哨兵错误到客户端 HTTP 响应的脱敏映射。
// 确保:已知业务错误透出明确类型/状态码;其余上游错误一律脱敏为 upstream_error(不泄漏内部信息)。
func TestWriteRelayErrMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		err      error
		wantCode int
		wantType string
	}{
		{"model_not_found", relay.ErrModelNotFound, http.StatusNotFound, "model_not_found"},
		{"no_channel", relay.ErrNoChannel, http.StatusServiceUnavailable, "no_channel"},
		{"insufficient_balance", relay.ErrInsufficientBal, http.StatusPaymentRequired, "insufficient_balance"},
		{"upstream_sanitized", errors.New("upstream 502: internal leak trace-id xyz"), http.StatusBadGateway, "upstream_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			g, _ := gin.CreateTestContext(w)
			g.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			(&Controller{}).writeRelayErr(g, tc.err)
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", w.Code, tc.wantCode)
			}
			var body struct {
				Error struct {
					Type string `json:"type"`
				} `json:"error"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body.Error.Type != tc.wantType {
				t.Fatalf("error.type = %q, want %q", body.Error.Type, tc.wantType)
			}
		})
	}
}

// TestWriteRelayErrPassthrough 验证 *canon.UpstreamError 在开关开启时原样透传上游
// status code + body + Retry-After + Content-Type,关闭时脱敏为 502 upstream_error(不泄漏上游内容)。
func TestWriteRelayErrPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamErr := &canon.UpstreamError{
		Provider:    "openai",
		StatusCode:  http.StatusTooManyRequests,
		Body:        []byte(`{"error":{"type":"rate_limit_error","message":"too many requests"}}`),
		ContentType: "application/json",
		RetryAfter:  "30",
	}

	t.Run("passthrough_on", func(t *testing.T) {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c := &Controller{Relay: &relay.Service{PassthroughUpstreamErrors: true}}
		c.writeRelayErr(g, upstreamErr)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", w.Code)
		}
		if !strings.Contains(w.Body.String(), "rate_limit_error") {
			t.Fatalf("body = %q, want to contain upstream body", w.Body.String())
		}
		if got := w.Header().Get("Retry-After"); got != "30" {
			t.Fatalf("Retry-After = %q, want 30", got)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
	})

	t.Run("passthrough_off_sanitized", func(t *testing.T) {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		c := &Controller{Relay: &relay.Service{PassthroughUpstreamErrors: false}}
		c.writeRelayErr(g, upstreamErr)
		if w.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", w.Code)
		}
		if strings.Contains(w.Body.String(), "rate_limit_error") {
			t.Fatalf("body leaked upstream content: %q", w.Body.String())
		}
	})

	// Relay 为 nil(零值 Controller,如未注入)时不应 panic,且走脱敏兜底。
	t.Run("nil_relay_sanitized", func(t *testing.T) {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		(&Controller{}).writeRelayErr(g, upstreamErr)
		if w.Code != http.StatusBadGateway {
			t.Fatalf("status = %d, want 502", w.Code)
		}
	})
}
