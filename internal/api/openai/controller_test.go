package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
