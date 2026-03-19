package gateway

import (
	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"sync"
	"time"
)

type ackService struct {
	mu      sync.Mutex
	pending map[string]pendingMsg
	midMap  map[int64]map[string]struct{}
	conf    *conf.ACK
}

type pendingMsg struct {
	mid  int64
	done chan struct{}
}

func newAckService(conf *conf.ACK) *ackService {
	return &ackService{
		pending: make(map[string]pendingMsg),
		midMap:  make(map[int64]map[string]struct{}),
		conf:    conf,
	}
}

func (as *ackService) makePending(msgID string, mid int64) <-chan struct{} {
	as.mu.Lock()
	defer as.mu.Unlock()

	if as.midMap[mid] == nil {
		as.midMap[mid] = make(map[string]struct{})
	}
	as.midMap[mid][msgID] = struct{}{}

	ch := make(chan struct{})
	as.pending[msgID] = pendingMsg{mid: mid, done: ch}
	return ch
}

func (as *ackService) removePending(msgID string) {
	as.mu.Lock()
	defer as.mu.Unlock()
	pm, exists := as.pending[msgID]
	if !exists {
		return
	}
	close(pm.done)
	delete(as.pending, msgID)
	delete(as.midMap[pm.mid], msgID)
	if len(as.midMap[pm.mid]) == 0 {
		delete(as.midMap, pm.mid)
	}
}

func (as *ackService) clearMid(mid int64) {
	as.mu.Lock()
	defer as.mu.Unlock()
	mm, exists := as.midMap[mid]
	if !exists {
		return
	}
	for msgID := range mm {
		close(as.pending[msgID].done)
		delete(as.pending, msgID)
	}
	delete(as.midMap, mid)
}

// Track is a goroutine
func (as *ackService) Track(msgID string, mid int64, op int32, body []byte, push func(int32, int64, []byte)) {
	numSecond := time.Duration(as.conf.RetryInterval)

	done := as.makePending(msgID, mid)

	for l := as.conf.MaxRetries; l > 0; l -= 1 {
		timer := time.NewTimer(numSecond * time.Second)
		select {
		case <-done:
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
			if l > 1 {
				push(op, mid, body)
			}
		}
	}
	as.removePending(msgID)
}

func (g *Gateway) Ack(msgID string) {
	g.ack.removePending(msgID)
}

func (g *Gateway) UserOffline(mid int64) {
	g.ack.clearMid(mid)
}
