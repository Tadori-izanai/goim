docker run -d --name=prometheus \
    -p 9090:9090 \
    -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
    prom/prometheus

# 之后打开 http://localhost:9090，在搜索框输入指标名就能查了：
#
# - goim_comet_online_connections — 直接看当前值
# - rate(goim_comet_push_messages_total[1m]) — 每秒推送速率
# - rate(goim_comet_push_messages_dropped_total[1m]) — 每秒丢弃速率
# - rate(goim_job_consume_messages_total[1m]) — MQ 消费速率
# - histogram_quantile(0.99, rate(goim_job_push_duration_seconds_bucket[5m])) — 推送延迟 P99
#
# 在 Graph 标签页可以看到时序图表。
