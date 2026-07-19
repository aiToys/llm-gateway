// Package metrics 提供 Prometheus 指标采集与 /metrics 端点。
//
// 指标设计(对标 Helicone/one-api 的运维最小集):
//
//	llm_requests_total{tenant,model,provider,status}  — 请求计数(Counter)
//	llm_request_duration_seconds{provider}            — 端到端延迟(Histogram)
//	llm_tokens_total{tenant,model,kind}               — token 计数(kind=prompt|completion)
//	llm_charge_cents_total{type}                      — 计费金额(分,type=usage|recharge|refund)
//	llm_channel_up{channel,provider}                  — 渠道熔断状态 1=可用 0=熔断(Gauge)
//	llm_inflight_requests                             — 在途请求数(Gauge)
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Requests 请求总数,按租户/模型/供应商/结果状态拆维。
	Requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_requests_total", Help: "Total LLM requests processed.",
	}, []string{"tenant", "model", "provider", "status"})

	// Duration 端到端请求延迟分布。
	Duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "llm_request_duration_seconds",
		Help:    "End-to-end request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider"})

	// Tokens 输入/输出 token 计数。
	Tokens = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_tokens_total", Help: "Total tokens processed.",
	}, []string{"tenant", "model", "kind"})

	// Charge 计费金额(单位:分),按账目类型拆维。
	Charge = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_charge_cents_total", Help: "Total charged/recharged/refunded amount in cents.",
	}, []string{"type"})

	// ChannelUp 渠道熔断状态(1=放行中,0=熔断打开)。
	ChannelUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "llm_channel_up", Help: "Channel circuit-breaker state: 1=open-allowing, 0=tripped.",
	}, []string{"channel"})

	// Inflight 在途请求数。
	Inflight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "llm_inflight_requests", Help: "In-flight LLM requests.",
	})

	// ChargeAbandoned 计费重试耗尽被放弃的笔数(漏账风险,需人工对账 + 告警)。
	ChargeAbandoned = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "llm_billing_charges_abandoned_total", Help: "Charges abandoned after exhausting retries (money leak risk).",
	})

	// ChargeEnqueueFail 计费失败入队也失败(全域 PG 不可达,真漏账)。需告警。
	ChargeEnqueueFail = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "llm_billing_charges_enqueue_fail_total", Help: "Charge failures that could not be enqueued for retry (unrecoverable leak).",
	})
)

func init() {
	prometheus.MustRegister(Requests, Duration, Tokens, Charge, ChannelUp, Inflight, ChargeAbandoned, ChargeEnqueueFail)
}

// Handler 暴露 /metrics(Prometheus 抓取)。
func Handler() http.Handler {
	return promhttp.Handler()
}

// ObserveRequest 记录一次请求的延迟与计数(relay 在请求结束后调用)。
func ObserveRequest(tenant, model, provider, status string, start time.Time, prompt, completion int) {
	Requests.WithLabelValues(tenant, model, provider, status).Inc()
	Duration.WithLabelValues(provider).Observe(time.Since(start).Seconds())
	if prompt > 0 {
		Tokens.WithLabelValues(tenant, model, "prompt").Add(float64(prompt))
	}
	if completion > 0 {
		Tokens.WithLabelValues(tenant, model, "completion").Add(float64(completion))
	}
}

// ObserveCharge 记录计费金额(分)。
func ObserveCharge(typ string, cents int64) {
	if cents != 0 {
		Charge.WithLabelValues(typ).Add(float64(cents))
	}
}

// ObserveChargeAbandoned 计费重试耗尽被放弃(漏账风险),供告警。
func ObserveChargeAbandoned() {
	ChargeAbandoned.Inc()
}

// ObserveChargeEnqueueFail 计费失败入队也失败(全域 PG 不可达,真漏账),供告警。
func ObserveChargeEnqueueFail() {
	ChargeEnqueueFail.Inc()
}
