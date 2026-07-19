package files

import "testing"

func TestIsAllowedMIME(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/webp", true},
		{"application/pdf", true},
		{"text/plain", true},
		{"image/png; charset=utf-8", true}, // 应剥离参数后匹配
		{"", false},                        // 空拒绝
		{"  ", false},
		{"application/x-msdownload", false}, // 可执行/危险类型拒绝
		{"text/html", false},                // 防 XSS: HTML 不在白名单
		{"image/svg+xml", false},            // SVG 可含脚本,默认不放行
	}
	for _, c := range cases {
		if got := isAllowedMIME(c.ct); got != c.want {
			t.Errorf("isAllowedMIME(%q)=%v, want %v", c.ct, got, c.want)
		}
	}
}
