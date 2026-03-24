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
	// background load (bench-mq)
	bgConns = flag.Int("bg-conns", 5000, "background: TCP connections to Comet")
	bgRate  = flag.Int("bg-rate", 200, "background: room push msg/s")

	// chat test
	chatPairs = flag.Int("pairs", 200, "chat: number of user pairs")
	chatRate  = flag.Int("rate", 50, "chat: msg/s")

	// common
	dur     = flag.Duration("duration", 60*time.Second, "test duration (chat), bg runs duration+20s")
	comet   = flag.String("comet", "localhost:3101", "Comet TCP address")
	logic   = flag.String("logic", "localhost:3111", "Logic HTTP address")
	gateway = flag.String("gateway", "http://localhost:3200", "Gateway HTTP address")
	ackFlag = flag.Bool("ack", true, "enable ACK")
	room    = flag.String("room", "1", "room ID for background load")
)

var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        2000,
		MaxIdleConnsPerHost: 2000,
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

type benchUser struct {
	ID    int64
	Token string
}

func main() {
	flag.Parse()
	log.Printf("bench-chat-load: bg=%d conns × %d msg/s, chat=%d pairs × %d msg/s, ack=%v, duration=%s",
		*bgConns, *bgRate, *chatPairs, *chatRate, *ackFlag, *dur)

	// ===== Phase 1: Setup all connections =====

	// 1a. Background load: connect to room
	log.Println("[setup] connecting background clients...")
	bgClients := make([]*pkg.TcpClient, 0, *bgConns)
	bgMetrics := pkg.NewMetrics()
	expectedBg := int64(*bgConns) * int64(*bgRate) * int64(dur.Seconds()+20)
	if expectedBg > 1000000 {
		bgMetrics.SetSampleRate(expectedBg / 1000000)
	}
	var bgWg sync.WaitGroup
	for i := 0; i < *bgConns; i++ {
		mid := int64(100000 + i)
		c, err := pkg.NewTcpClient(*comet, mid, "test://"+*room, []int32{1000})
		if err != nil {
			log.Fatalf("bg connect %d: %v", i, err)
		}
		bgClients = append(bgClients, c)
		bgWg.Add(1)
		go func(tc *pkg.TcpClient) {
			defer bgWg.Done()
			tc.Receive(func(op int32, body []byte) {
				var msg benchMsg
				if err := json.Unmarshal(body, &msg); err == nil && msg.Ts > 0 {
					bgMetrics.RecordLatency(msg.Ts)
				}
			})
		}(c)
	}
	log.Printf("[setup] %d background clients connected", *bgConns)

	// 1b. Chat: register, login, add friends
	log.Println("[setup] setting up chat users...")
	chatUsers := setupChatUsers(*chatPairs * 2)
	log.Printf("[setup] %d chat users ready", len(chatUsers))

	// 1c. Chat: connect receivers
	chatMetrics := pkg.NewMetrics()
	var seen sync.Map
	var chatStarted atomic.Bool
	chatClients := make([]*pkg.TcpClient, 0, *chatPairs)
	var chatWg sync.WaitGroup
	for i := 0; i < *chatPairs; i++ {
		receiver := chatUsers[i*2+1]
		c, err := pkg.NewTcpClient(*comet, receiver.ID, "", []int32{OpSingleChatMsg})
		if err != nil {
			log.Fatalf("chat connect receiver %d (mid=%d): %v", i, receiver.ID, err)
		}
		chatClients = append(chatClients, c)
		chatWg.Add(1)
		go func(tc *pkg.TcpClient) {
			defer chatWg.Done()
			tc.Receive(func(op int32, body []byte) {
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
				if *ackFlag && msg.MsgID != "" {
					ackMsg(msg.MsgID)
				}
				if !chatStarted.Load() {
					return
				}
				if msg.MsgID != "" {
					if _, loaded := seen.LoadOrStore(msg.MsgID, struct{}{}); loaded {
						return
					}
				}
				ts, err := strconv.ParseInt(msg.Content, 10, 64)
				if err != nil || ts <= 0 {
					return
				}
				chatMetrics.RecordLatency(ts)
			})
		}(c)
	}
	log.Printf("[setup] %d chat receivers connected", *chatPairs)

	// drain old chat messages
	log.Println("[setup] draining old messages (2s)...")
	time.Sleep(2 * time.Second)

	log.Println("========== ALL CONNECTIONS READY ==========")

	// ===== Phase 2: Start background load =====
	log.Println("[phase 2] starting background room push...")
	bgMetrics.StartLiveReport(5 * time.Second)
	bgStop := make(chan struct{})
	var bgSeq atomic.Int64
	go func() {
		interval := time.Second / time.Duration(*bgRate)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		url := fmt.Sprintf("http://%s/goim/push/room?operation=1000&type=test&room=%s", *logic, *room)
		for {
			select {
			case <-bgStop:
				return
			case <-ticker.C:
				s := bgSeq.Add(1)
				msg, _ := json.Marshal(&benchMsg{Seq: s, Ts: time.Now().UnixNano()})
				go func() {
					bgMetrics.IncSentGroup(int64(*bgConns))
					resp, err := httpClient.Post(url, "application/json", bytes.NewReader(msg))
					if err != nil {
						//bgMetrics.IncSentGroup(-int64(*bgConns))
						return
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}()
			}
		}
	}()

	// ===== Phase 3: Wait 8s, then start chat =====
	log.Println("[phase 3] waiting 8s for background load to stabilize...")
	time.Sleep(8 * time.Second)

	log.Println("[phase 3] starting chat test...")
	chatStarted.Store(true)
	chatMetrics.StartLiveReport(5 * time.Second)
	chatStop := make(chan struct{})
	var chatSeq atomic.Int64
	go func() {
		interval := time.Second / time.Duration(*chatRate)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-chatStop:
				return
			case <-ticker.C:
				idx := int(chatSeq.Add(1)-1) % *chatPairs
				sender := chatUsers[idx*2]
				receiver := chatUsers[idx*2+1]
				ts := time.Now().UnixNano()
				go sendChat(sender.Token, receiver.ID, strconv.FormatInt(ts, 10), chatMetrics)
			}
		}
	}()

	// ===== Phase 4: Wait for chat duration, then stop =====
	time.Sleep(*dur)
	close(chatStop)
	log.Println("[phase 4] chat sending stopped, waiting 3s for in-flight...")
	time.Sleep(3 * time.Second)

	// stop background
	close(bgStop)
	log.Println("[phase 4] background sending stopped, waiting 2s...")
	time.Sleep(2 * time.Second)

	// close all
	for _, c := range chatClients {
		c.Close()
	}
	for _, c := range bgClients {
		c.Close()
	}
	chatWg.Wait()
	bgWg.Wait()

	// ===== Phase 5: Reports =====
	fmt.Println()
	log.Println("===== Chat Report =====")
	chatMetrics.Report()

	fmt.Println()
	log.Println("===== Background Load Report =====")
	bgMetrics.Report()
}

// --- Chat setup (idempotent, concurrent) ---

func setupChatUsers(n int) []benchUser {
	users := make([]benchUser, n)
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
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
	m.IncSent()
	resp, err := httpClient.Do(req)
	if err != nil {
		//m.IncSentGroup(-1)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func ackMsg(msgID string) {
	resp, err := httpClient.Post(*gateway+"/goim/ack/"+msgID, "", nil)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}
