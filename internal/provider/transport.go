package provider

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewClient 构造防 SSRF 的 http.Client,供各上游 adapter 复用。
//
//	- Transport.DialContext: 每次建连前 resolve 主机,命中内网/回环/链路本地直接拒绝(防 DNS rebinding--
//	  渠道写入时 base_url 校验通过,但请求时 DNS 返回 169.254.169.254 等内网 IP 的攻击)。
//	- CheckRedirect: 每次重定向后重新校验目标(防 302 -> http://169.254.169.254 云元数据)。
//
// keepAlives=false 禁用连接复用(部分供应商关闭空闲 keep-alive 致 EOF 复用问题);
// rspHeaderTimeout 约束响应头到达时间(流式 body 读取阶段不受限);timeout<=0 表示无整体超时(流式用,
// 由请求 ctx 控制生命周期)。dev=true 放宽(便于本地 mock 上游)。
func NewClient(dev bool, keepAlives bool, rspHeaderTimeout, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: newTransport(dev, keepAlives, rspHeaderTimeout),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if !dev && !isPublicHost(req.Context(), req.URL.Hostname()) {
				return fmt.Errorf("redirect to non-public host %s blocked (SSRF)", req.URL.Hostname())
			}
			return nil
		},
	}
}

func newTransport(dev bool, keepAlives bool, rspHeaderTimeout time.Duration) *http.Transport {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		DisableKeepAlives:     !keepAlives,
		ResponseHeaderTimeout: rspHeaderTimeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			if !dev && !isPublicHost(ctx, host) {
				return nil, fmt.Errorf("upstream %s resolves to non-public address, blocked (SSRF)", host)
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
}

// isPublicHost 解析 host,所有解析 IP 均须为公网地址才放行。
// 任一 IP 命中回环/私网/链路本地/未指定即拒绝;DNS 解析失败或无结果也拒绝(fail-closed)。
func isPublicHost(ctx context.Context, host string) bool {
	if host == "" {
		return false
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !isPublicIP(ip.IP) {
			return false
		}
	}
	return true
}

// isPublicIP 判断 IP 是否为可路由的公网单播地址。
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() {
		return false
	}
	return ip.IsGlobalUnicast()
}
