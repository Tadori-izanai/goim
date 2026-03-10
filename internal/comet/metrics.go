package comet

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// OnlineConnections 当前在线连接数（TCP + WebSocket）。
	// Gauge 类型：连接建立时 Inc，断开时 Dec。
	OnlineConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "goim",
		Subsystem: "comet",
		Name:      "online_connections",
		Help:      "Current number of online connections.",
	})

	// PushMessagesTotal 推送消息累计总数。
	// Counter 类型：每次 gRPC handler 收到推送请求时 Inc。
	PushMessagesTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "goim",
		Subsystem: "comet",
		Name:      "push_messages_total",
		Help:      "Total number of pushed messages.",
	})

	// PushMessagesDroppedTotal 因 signal channel 满而丢弃的消息总数。
	// Counter 类型：Channel.Push() 走到 default 分支时 Inc。
	PushMessagesDroppedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "goim",
		Subsystem: "comet",
		Name:      "push_messages_dropped_total",
		Help:      "Total number of messages dropped due to full signal channel.",
	})
)

func init() {
	prometheus.MustRegister(OnlineConnections, PushMessagesTotal, PushMessagesDroppedTotal)
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
