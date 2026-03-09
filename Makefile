# Go parameters
GOCMD=GO111MODULE=on go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test

all: test build
build:
	rm -rf target/
	mkdir target/
	cp cmd/comet/comet-example.toml target/comet.toml
	cp cmd/logic/logic-example.toml target/logic.toml
	cp cmd/job/job-example.toml target/job.toml
	$(GOBUILD) -o target/comet cmd/comet/main.go
	$(GOBUILD) -o target/logic cmd/logic/main.go
	$(GOBUILD) -o target/job cmd/job/main.go

test:
	$(GOTEST) -v ./...

clean:
	rm -rf target/

run:
	nohup target/logic -conf=target/logic.toml -region=sh -zone=sh001 -deploy.env=dev -weight=10 2>&1 > target/logic.log &
	nohup target/comet -conf=target/comet.toml -region=sh -zone=sh001 -deploy.env=dev -weight=10 -addrs=127.0.0.1 -debug=true 2>&1 > target/comet.log &
	nohup target/job -conf=target/job.toml -region=sh -zone=sh001 -deploy.env=dev 2>&1 > target/job.log &

stop:
	pkill -f target/logic
	pkill -f target/job
	pkill -f target/comet

# --- 单个网元 ---

comet:
	target/comet -conf=target/comet.toml -region=sh -zone=sh001 -deploy.env=dev -weight=10 -addrs=127.0.0.1 -debug=true -alsologtostderr 2>&1 | tee target/comet.log

logic:
	target/logic -conf=target/logic.toml -region=sh -zone=sh001 -deploy.env=dev -weight=10 -alsologtostderr 2>&1 | tee target/logic.log

job:
	target/job -conf=target/job.toml -region=sh -zone=sh001 -deploy.env=dev -alsologtostderr 2>&1 | tee target/job.log

# --- 压测工具 ---

BENCH_COMET ?= localhost:3101
BENCH_LOGIC ?= localhost:3111
BENCH_CLIENTS ?= 10000
BENCH_ROOM ?= 1
BENCH_RATE ?= 40
BENCH_DURATION ?= 60

bench-build:
	$(GOBUILD) -o target/bench-client benchmarks/client/main.go
	$(GOBUILD) -o target/bench-push benchmarks/push/main.go
	$(GOBUILD) -o target/bench-push-room benchmarks/push_room/main.go

bench-client:
	target/bench-client 1 $(BENCH_CLIENTS) $(BENCH_COMET) -alsologtostderr 2>&1 | tee target/bench-client.log

bench-push:
	target/bench-push 0 $(BENCH_CLIENTS) $(BENCH_LOGIC) $(BENCH_DURATION) 2>&1 | tee target/bench-push.log

bench-push-room:
	target/bench-push-room $(BENCH_ROOM) $(BENCH_RATE) $(BENCH_LOGIC) 2>&1 | tee target/bench-push-room.log
