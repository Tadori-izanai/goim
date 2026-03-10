package job

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// ConsumeMessagesTotal MQ 消费消息累计总数。
	ConsumeMessagesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "goim",
		Subsystem: "job",
		Name:      "consume_messages_total",
		Help:      "Total number of consumed MQ messages.",
	})

	// PushDurationSeconds 推送延迟分布。
	PushDurationSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "goim",
		Subsystem: "job",
		Name:      "push_duration_seconds",
		Help:      "Push latency distribution in seconds.",
		Buckets:   []float64{0.0001, 0.00025, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
	})
)

func init() {
	prometheus.MustRegister(ConsumeMessagesTotal, PushDurationSeconds)
}

// InitMetrics 在独立端口启动 HTTP 服务，暴露 /metrics 端点供 Prometheus 拉取。
func InitMetrics(addr string) {
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(addr, mux); err != nil {
			panic(err)
		}
	}()
}
