package config

import (
	"encoding/hex"
	"testing"
)

func TestValidateRejectsWeakSecretsInProduction(t *testing.T) {
	cases := []struct {
		name    string
		jwt     string
		master  string
		wantErr bool
	}{
		{"empty jwt", "", randHex(t), true},
		{"example jwt", "change-me-in-production-please-use-32-bytes-or-more", randHex(t), true},
		{"short jwt", "short", randHex(t), true},
		{"empty master", rand32(t), "", true},
		{"example master", rand32(t), "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{"non-hex master", rand32(t), "not-hex-at-all-but-long-enough-xxxxxxxxxxxxxxxxx", true},
		{"valid", rand32(t), randHex(t), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Auth: Auth{JWTSecret: tc.jwt, ChannelKeyMaster: tc.master}} // Dev=false
			err := c.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
		})
	}
}

func TestValidateDevFillsDefaults(t *testing.T) {
	c := &Config{Dev: true, Server: Server{Addr: "127.0.0.1:8080"}} // dev 需 loopback 监听
	if err := c.Validate(); err != nil {
		t.Fatalf("dev mode should never fail with loopback addr: %v", err)
	}
	if c.Auth.JWTSecret == "" {
		t.Fatal("dev mode should fill jwt secret")
	}
	if _, err := hex.DecodeString(c.Auth.ChannelKeyMaster); err != nil {
		t.Fatalf("dev mode should fill a valid hex master: %v", err)
	}
}

// TestValidateDevRejectsNonLoopback 验证 dev 模式仅允许 loopback 监听,
// 防止 mock 支付/弱密钥裸奔到公网(:8080/0.0.0.0/公网 IP 均拒绝)。
func TestValidateDevRejectsNonLoopback(t *testing.T) {
	cases := []struct{ name, addr string }{
		{"empty binds all", ""},
		{"all interfaces", ":8080"},
		{"wildcard v4", "0.0.0.0:8080"},
		{"wildcard v6", "[::]:8080"},
		{"public ip", "8.8.8.8:8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{Dev: true, Server: Server{Addr: tc.addr}}
			if err := c.Validate(); err == nil {
				t.Fatalf("dev with non-loopback addr %q should be rejected", tc.addr)
			}
		})
	}
	// loopback 应放行。
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		c := &Config{Dev: true, Server: Server{Addr: addr}}
		if err := c.Validate(); err != nil {
			t.Fatalf("dev loopback addr %q should pass: %v", addr, err)
		}
	}
}

// TestApplyEnvCoversFields 验证环境变量映射覆盖补齐的各段(ReqLog/Billing/Log/Edge/Web/Defaults/CORS),
// 避免 k8s/容器部署时这些字段成为配置盲区。
func TestApplyEnvCoversFields(t *testing.T) {
	t.Setenv("GATEWAY_REQ_LOG__ENABLED", "true")
	t.Setenv("GATEWAY_REQ_LOG__SAMPLE_RATE", "0.5")
	t.Setenv("GATEWAY_REQ_LOG__MAX_BODY_BYTES", "1024")
	t.Setenv("GATEWAY_REQ_LOG__RETAIN_DAYS", "30")
	t.Setenv("GATEWAY_REQ_LOG__LOG_BODIES", "false")
	t.Setenv("GATEWAY_BILLING__MIN_BALANCE_CENTS", "500")
	t.Setenv("GATEWAY_BILLING__CHARS_PER_TOKEN", "3")
	t.Setenv("GATEWAY_LOG__LEVEL", "debug")
	t.Setenv("GATEWAY_LOG__FORMAT", "json")
	t.Setenv("GATEWAY_DEFAULTS__DEFAULT_PROVIDER", "openaicomp")
	t.Setenv("GATEWAY_WEB__USER_DIST", "/x/user")
	t.Setenv("GATEWAY_EDGE__ADDR", ":8090")
	t.Setenv("GATEWAY_EDGE__STANDALONE", "true")
	t.Setenv("GATEWAY_SERVER__CORS_ORIGINS", "https://a.com, https://b.com")
	t.Setenv("GATEWAY_POSTGRES__MAX_CONNS", "20")

	c := defaults()
	applyEnv(c)

	if !c.ReqLog.Enabled || c.ReqLog.SampleRate != 0.5 || c.ReqLog.MaxBodyBytes != 1024 ||
		c.ReqLog.RetainDays != 30 || c.ReqLog.LogBodies {
		t.Fatalf("req_log env not applied: %+v", c.ReqLog)
	}
	if c.Billing.MinBalanceCents != 500 || c.Billing.CharsPerToken != 3 {
		t.Fatalf("billing env not applied: %+v", c.Billing)
	}
	if c.Log.Level != "debug" || c.Log.Format != "json" {
		t.Fatalf("log env not applied: %+v", c.Log)
	}
	if c.Defaults.DefaultProvider != "openaicomp" {
		t.Fatalf("defaults env not applied: %s", c.Defaults.DefaultProvider)
	}
	if c.Web.UserDist != "/x/user" {
		t.Fatalf("web env not applied: %+v", c.Web)
	}
	if c.Edge.Addr != ":8090" || !c.Edge.Standalone {
		t.Fatalf("edge env not applied: %+v", c.Edge)
	}
	if len(c.Server.CORSOrigins) != 2 || c.Server.CORSOrigins[0] != "https://a.com" {
		t.Fatalf("cors env not applied: %+v", c.Server.CORSOrigins)
	}
	if c.Postgres.MaxConns != 20 {
		t.Fatalf("postgres max_conns env not applied: %d", c.Postgres.MaxConns)
	}
}

func rand32(t *testing.T) string {
	t.Helper()
	// 32 字符的合法 jwt(>=32)
	return "0123456789abcdef0123456789abcdef"
}

func randHex(t *testing.T) string {
	t.Helper()
	return "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
}
