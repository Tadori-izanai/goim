package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Terry-Mao/goim/benchmarks/pkg"
)

var (
	conns    = flag.Int("conns", 500, "number of TCP connections to Comet")
	rate     = flag.Int("rate", 10, "messages per second")
	duration = flag.Duration("duration", 60*time.Second, "test duration")
	comet    = flag.String("comet", "localhost:3101", "Comet TCP address")
	logic    = flag.String("logic", "localhost:3111", "Logic HTTP address")
	room     = flag.String("room", "1", "room ID")
)

var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        1000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
	Timeout: 10 * time.Second,
}

type benchMsg struct {
	Seq int64 `json:"seq"`
	Ts  int64 `json:"ts"`
}

func main() {
	flag.Parse()
	log.Printf("bench-mq: conns=%d rate=%d duration=%s", *conns, *rate, *duration)

	metrics := pkg.NewMetrics()

	// 1. connect receivers
	var wg sync.WaitGroup
	clients := make([]*pkg.TcpClient, 0, *conns)
	for i := 0; i < *conns; i++ {
		mid := int64(100000 + i)
		c, err := pkg.NewTcpClient(*comet, mid, "test://"+*room, []int32{1000})
		if err != nil {
			log.Fatalf("connect %d: %v", i, err)
		}
		clients = append(clients, c)
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Receive(func(op int32, body []byte) {
				var msg benchMsg
				if err := json.Unmarshal(body, &msg); err == nil && msg.Ts > 0 {
					metrics.RecordLatency(msg.Ts)
				}
			})
		}()
	}
	log.Printf("all %d connections established", *conns)

	// 2. start live report
	metrics.StartLiveReport(5 * time.Second)

	// 3. send messages
	var seq atomic.Int64
	stopCh := make(chan struct{})
	go func() {
		interval := time.Second / time.Duration(*rate)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		url := fmt.Sprintf("http://%s/goim/push/room?operation=1000&type=test&room=%s", *logic, *room)
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				s := seq.Add(1)
				msg, _ := json.Marshal(&benchMsg{Seq: s, Ts: time.Now().UnixNano()})
				go func() {
					resp, err := httpClient.Post(url, "application/json", bytes.NewReader(msg))
					if err != nil {
						return
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					metrics.IncSentGroup(int64(*conns))
				}()
			}
		}
	}()

	// 4. wait for duration then stop
	time.Sleep(*duration)
	close(stopCh)
	log.Println("sending stopped, waiting for in-flight messages...")
	time.Sleep(2 * time.Second)

	// 5. close all clients
	for _, c := range clients {
		c.Close()
	}
	wg.Wait()

	// 6. report
	metrics.Report()
}
