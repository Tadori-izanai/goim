package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Terry-Mao/goim/benchmarks/pkg"
)

const OpGroupChatMsg = int32(2002)

var (
	members = flag.Int("members", 100, "number of group members (receivers)")
	rate    = flag.Int("rate", 10, "messages per second")
	dur     = flag.Duration("duration", 30*time.Second, "test duration")
	gwAddr  = flag.String("gateway", "http://localhost:3200", "Gateway HTTP address")
	comet   = flag.String("comet", "localhost:3101", "Comet TCP address")
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

// fanoutRecord tracks per-message fanout latencies.
type fanoutRecord struct {
	mu   sync.Mutex
	lats []int64 // one per receiver, nanoseconds
}

func main() {
	flag.Parse()
	log.Printf("bench-fanout: members=%d rate=%d duration=%s", *members, *rate, *dur)

	// 1. Setup: register, login, create group, join
	totalUsers := *members + 1
	users := setupUsers(totalUsers)
	sender := users[0]
	receivers := users[1:]
	groupID := setupGroup(sender, receivers)
	log.Printf("setup complete: %d users, group=%d", totalUsers, groupID)

	metrics := pkg.NewMetrics()
	fanouts := &sync.Map{} // seq -> *fanoutRecord

	// 2. Connect receivers via TCP
	var wg sync.WaitGroup
	clients := make([]*pkg.TcpClient, 0, *members)
	for i := 0; i < len(receivers); i++ {
		c, err := pkg.NewTcpClient(*comet, receivers[i].ID, "", []int32{OpGroupChatMsg})
		if err != nil {
			log.Fatalf("connect receiver %d (mid=%d): %v", i, receivers[i].ID, err)
		}
		clients = append(clients, c)
		wg.Add(1)
		go func(tc *pkg.TcpClient) {
			defer wg.Done()
			tc.Receive(func(op int32, body []byte) {
				if op != OpGroupChatMsg {
					return
				}
				var msg struct {
					Content string `json:"content"`
				}
				if err := json.Unmarshal(body, &msg); err != nil {
					return
				}
				parts := strings.SplitN(msg.Content, ":", 2)
				if len(parts) != 2 {
					return
				}
				seq, err1 := strconv.ParseInt(parts[0], 10, 64)
				ts, err2 := strconv.ParseInt(parts[1], 10, 64)
				if err1 != nil || err2 != nil || ts <= 0 {
					return
				}
				lat := time.Now().UnixNano() - ts
				if lat < 0 {
					lat = 0
				}
				metrics.RecordLatency(ts)

				val, _ := fanouts.LoadOrStore(seq, &fanoutRecord{})
				rec := val.(*fanoutRecord)
				rec.mu.Lock()
				rec.lats = append(rec.lats, lat)
				rec.mu.Unlock()
			})
		}(c)
	}
	log.Printf("all %d receivers connected", *members)

	// 3. Start live report
	metrics.StartLiveReport(5 * time.Second)

	// 4. Send messages
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
				s := seq.Add(1)
				ts := time.Now().UnixNano()
				content := fmt.Sprintf("%d:%d", s, ts)
				go sendGroupChat(sender.Token, groupID, content, metrics)
			}
		}
	}()

	// 5. Wait then stop
	time.Sleep(*dur)
	close(stopCh)
	log.Println("sending stopped, waiting for in-flight messages...")
	time.Sleep(3 * time.Second)

	for _, c := range clients {
		c.Close()
	}
	wg.Wait()

	// 6. Report
	metrics.Report()
	reportFanout(fanouts, *members)
}

// --- Setup (idempotent) ---

func setupUsers(n int) []benchUser {
	users := make([]benchUser, n)
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			username := fmt.Sprintf("bench_fanout_%d", idx)
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
	return users
}

func setupGroup(sender benchUser, receivers []benchUser) int64 {
	groupID := createGroup(sender.Token, fmt.Sprintf("bench_fanout_%d", len(receivers)))
	joinGroup(sender.Token, groupID)
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for i := range receivers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			joinGroup(receivers[idx].Token, groupID)
		}(i)
	}
	wg.Wait()
	return groupID
}

// --- Gateway API helpers ---

type apiResp struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func register(username, password string) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := httpClient.Post(*gwAddr+"/goim/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func login(username, password string) (int64, string, error) {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := httpClient.Post(*gwAddr+"/goim/auth/login", "application/json", bytes.NewReader(body))
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

func createGroup(token, name string) int64 {
	body, _ := json.Marshal(map[string]string{"name": name})
	req, _ := http.NewRequest("POST", *gwAddr+"/goim/group", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("create group: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		log.Fatalf("create group unmarshal: %v, body: %s", err, raw)
	}
	var data struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(r.Data, &data); err != nil {
		log.Fatalf("create group data: %v", err)
	}
	return data.ID
}

func joinGroup(token string, groupID int64) {
	url := fmt.Sprintf("%s/goim/group/%d/join", *gwAddr, groupID)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func sendGroupChat(token string, groupID int64, content string, m *pkg.Metrics) {
	body, _ := json.Marshal(map[string]any{
		"content_type": 1,
		"content":      content,
	})
	url := fmt.Sprintf("%s/goim/group/%d/chat", *gwAddr, groupID)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	m.IncSentGroup(int64(*members))
}

// --- Fanout report ---

func reportFanout(fanouts *sync.Map, memberCount int) {
	var firsts, lasts, avgs []int64

	fanouts.Range(func(_, val any) bool {
		rec := val.(*fanoutRecord)
		rec.mu.Lock()
		lats := make([]int64, len(rec.lats))
		copy(lats, rec.lats)
		rec.mu.Unlock()

		if len(lats) == 0 {
			return true
		}
		sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
		firsts = append(firsts, lats[0])
		lasts = append(lasts, lats[len(lats)-1])
		var sum int64
		for _, l := range lats {
			sum += l
		}
		avgs = append(avgs, sum/int64(len(lats)))
		return true
	})

	sort.Slice(firsts, func(i, j int) bool { return firsts[i] < firsts[j] })
	sort.Slice(lasts, func(i, j int) bool { return lasts[i] < lasts[j] })
	sort.Slice(avgs, func(i, j int) bool { return avgs[i] < avgs[j] })

	sep := strings.Repeat("─", 44)
	fmt.Println()
	fmt.Println(sep)
	fmt.Printf("  Fanout Report  (messages=%d, members=%d)\n", len(firsts), memberCount)
	fmt.Println(sep)
	printPercentiles("  First (ms)", firsts)
	printPercentiles("  Last  (ms)", lasts)
	printPercentiles("  Avg   (ms)", avgs)
	fmt.Println(sep)
}

func printPercentiles(label string, sorted []int64) {
	if len(sorted) == 0 {
		return
	}
	fmt.Printf("%s:  P50=%.1f  P95=%.1f  P99=%.1f  Max=%.1f\n",
		label,
		pctMs(sorted, 0.50),
		pctMs(sorted, 0.95),
		pctMs(sorted, 0.99),
		float64(sorted[len(sorted)-1])/1e6,
	)
}

func pctMs(sorted []int64, p float64) float64 {
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return float64(sorted[idx]) / 1e6
}
