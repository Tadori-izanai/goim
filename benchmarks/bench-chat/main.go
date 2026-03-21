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
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Terry-Mao/goim/benchmarks/pkg"
)

const OpSingleChatMsg = int32(2001)

var (
	pairs   = flag.Int("pairs", 50, "number of user pairs")
	rate    = flag.Int("rate", 10, "messages per second")
	dur     = flag.Duration("duration", 60*time.Second, "test duration")
	gateway = flag.String("gateway", "http://localhost:3200", "Gateway HTTP address")
	comet   = flag.String("comet", "localhost:3101", "Comet TCP address")
	ackFlag = flag.Bool("ack", true, "enable ACK")
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

type benchUser struct {
	ID    int64
	Token string
}

func main() {
	flag.Parse()
	log.Printf("bench-chat: pairs=%d rate=%d duration=%s ack=%v", *pairs, *rate, *dur, *ackFlag)

	// 1. Setup: register, login, add friends (idempotent)
	users := setup(*pairs * 2)
	log.Printf("setup complete: %d users ready", len(users))

	metrics := pkg.NewMetrics()
	var seen sync.Map // msg_id dedup: ACK retries cause duplicate deliveries

	// 2. Connect receivers (odd-indexed) via TCP
	var wg sync.WaitGroup
	clients := make([]*pkg.TcpClient, 0, *pairs)
	for i := 0; i < *pairs; i++ {
		receiver := users[i*2+1]
		c, err := pkg.NewTcpClient(*comet, receiver.ID, "", []int32{OpSingleChatMsg})
		if err != nil {
			log.Fatalf("connect receiver %d (mid=%d): %v", i, receiver.ID, err)
		}
		clients = append(clients, c)
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Receive(func(op int32, body []byte) {
				if op != OpSingleChatMsg {
					return
				}
				var msg struct {
					MsgID   string `json:"msg_id"`
					Content string `json:"content"`
				}
				if err := json.Unmarshal(body, &msg); err != nil {
					return
				}
				// always ACK to stop retries
				if *ackFlag && msg.MsgID != "" {
					go ackMsg(msg.MsgID)
				}
				// dedup: only record latency on first delivery
				if msg.MsgID != "" {
					if _, loaded := seen.LoadOrStore(msg.MsgID, struct{}{}); loaded {
						return
					}
				}
				ts, err := strconv.ParseInt(msg.Content, 10, 64)
				if err != nil || ts <= 0 {
					return
				}
				metrics.RecordLatency(ts)
			})
		}()
	}
	log.Printf("all %d receivers connected", *pairs)

	// 3. Start live report
	metrics.StartLiveReport(5 * time.Second)

	// 4. Send messages: round-robin across pairs
	var seq atomic.Int64
	stopCh := make(chan struct{})
	go func() {
		interval := time.Second / time.Duration(*rate)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				idx := int(seq.Add(1)-1) % *pairs
				sender := users[idx*2]
				receiver := users[idx*2+1]
				ts := time.Now().UnixNano()
				go sendChat(sender.Token, receiver.ID, strconv.FormatInt(ts, 10), metrics)
			}
		}
	}()

	// 5. Wait then stop
	time.Sleep(*dur)
	close(stopCh)
	log.Println("sending stopped, waiting for in-flight messages...")
	time.Sleep(2 * time.Second)

	for _, c := range clients {
		c.Close()
	}
	wg.Wait()
	metrics.Report()
}

// setup registers and logs in users, adds friend pairs. Idempotent. Concurrent.
func setup(n int) []benchUser {
	users := make([]benchUser, n)
	sem := make(chan struct{}, 64) // concurrency limit
	var wg sync.WaitGroup

	// phase 1: register + login concurrently
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			username := fmt.Sprintf("bench_chat_%d", idx)
			password := "bench123"
			register(username, password)
			id, token, err := login(username, password)
			if err != nil {
				log.Fatalf("login user %s: %v", username, err)
			}
			users[idx] = benchUser{ID: id, Token: token}
		}(i)
	}
	wg.Wait()

	// phase 2: add friends concurrently
	for i := 0; i < n; i += 2 {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			addFriend(users[idx].Token, users[idx+1].ID)
		}(i)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			addFriend(users[idx+1].Token, users[idx].ID)
		}(i)
	}
	wg.Wait()
	return users
}

// --- Gateway API helpers ---

type apiResp struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func register(username, password string) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := httpClient.Post(*gateway+"/goim/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func login(username, password string) (int64, string, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := httpClient.Post(*gateway+"/goim/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return 0, "", fmt.Errorf("unmarshal: %w, body: %s", err, raw)
	}
	if r.Code != 0 {
		return 0, "", fmt.Errorf("login code=%d, body: %s", r.Code, raw)
	}
	var data struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return 0, "", err
	}
	return data.ID, data.Token, nil
}

func addFriend(token string, friendID int64) {
	url := fmt.Sprintf("%s/goim/friend/%d", *gateway, friendID)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func sendChat(token string, toID int64, content string, m *pkg.Metrics) {
	body, _ := json.Marshal(map[string]any{
		"to":           toID,
		"content_type": 1,
		"content":      content,
	})
	req, _ := http.NewRequest("POST", *gateway+"/goim/chat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	m.IncSent()
}

func ackMsg(msgID string) {
	resp, err := httpClient.Post(*gateway+"/goim/ack/"+msgID, "", nil)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
