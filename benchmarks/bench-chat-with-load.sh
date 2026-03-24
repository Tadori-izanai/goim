#!/bin/bash
# bench-chat-with-load.sh
# 先启动 bench-mq 造背景负载，再启动 bench-chat 测 ACK 开/关差异
#
# Usage:
#   ./benchmarks/bench-chat-with-load.sh -ack true  -comet 10.206.0.3:3101 -logic 10.206.0.3:3111 -gateway http://10.206.0.3:3200
#   ./benchmarks/bench-chat-with-load.sh -ack false -comet 10.206.0.3:3101 -logic 10.206.0.3:3111 -gateway http://10.206.0.3:3200

set -e

# Defaults
COMET="localhost:3101"
LOGIC="localhost:3111"
GATEWAY="http://localhost:3200"
ACK="true"

# Background load params (bench-mq)
BG_CONNS=5000
BG_RATE=300
BG_DURATION=90s

# Chat test params (bench-chat)
CHAT_PAIRS=200
CHAT_RATE=50
CHAT_DURATION=60s

while [[ $# -gt 0 ]]; do
  case $1 in
    -ack)      ACK="$2";     shift 2 ;;
    -comet)    COMET="$2";   shift 2 ;;
    -logic)    LOGIC="$2";   shift 2 ;;
    -gateway)  GATEWAY="$2"; shift 2 ;;
    *) echo "Unknown arg: $1"; exit 1 ;;
  esac
done

echo "=== bench-chat-with-load ==="
echo "  ACK:      $ACK"
echo "  BG load:  $BG_CONNS conns, $BG_RATE msg/s, $BG_DURATION"
echo "  Chat:     $CHAT_PAIRS pairs, $CHAT_RATE msg/s, $CHAT_DURATION"
echo ""

# 1. Start background load (bench-mq)
echo "[1/3] Starting background load (bench-mq)..."
go run benchmarks/bench-mq/main.go \
  -conns "$BG_CONNS" -rate "$BG_RATE" -duration "$BG_DURATION" \
  -comet "$COMET" -logic "$LOGIC" &
BG_PID=$!

# 2. Wait for background connections to establish
sleep 6
echo "[2/3] Background load running (PID=$BG_PID), starting bench-chat..."

# 3. Run bench-chat
go run benchmarks/bench-chat/main.go \
  -pairs "$CHAT_PAIRS" -rate "$CHAT_RATE" -duration "$CHAT_DURATION" \
  -comet "$COMET" -gateway "$GATEWAY" -ack="$ACK"

echo "[3/3] bench-chat finished, waiting for background load to complete..."
wait $BG_PID 2>/dev/null || true

echo "=== Done ==="
