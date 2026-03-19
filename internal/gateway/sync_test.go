package gateway

import (
	"context"
	"testing"
	"time"
)

func TestSyncMessages_SingleAndGroup(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")
	g.AddFriend(ctx, aliceID, bobID)

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")
	g.JoinGroup(ctx, group.ID, bobID)

	before := time.Now()
	g.SendMessage(ctx, aliceID, bobID, 1, "dm hello")
	g.SendGroupMessage(ctx, group.ID, aliceID, 1, "group hello")

	result, err := g.SyncMessages(ctx, bobID, before, 50)
	if err != nil {
		t.Fatalf("SyncMessages failed: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Content != "dm hello" {
		t.Fatalf("expected 'dm hello', got %q", result.Messages[0].Content)
	}
	if len(result.GroupMessages) != 1 {
		t.Fatalf("expected 1 group message, got %d", len(result.GroupMessages))
	}
	if result.GroupMessages[0].Content != "group hello" {
		t.Fatalf("expected 'group hello', got %q", result.GroupMessages[0].Content)
	}
}

func TestSyncMessages_Empty(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")

	result, err := g.SyncMessages(ctx, aliceID, time.Now(), 50)
	if err != nil {
		t.Fatalf("SyncMessages failed: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(result.Messages))
	}
	if len(result.GroupMessages) != 0 {
		t.Fatalf("expected 0 group messages, got %d", len(result.GroupMessages))
	}
}

func TestSyncMessages_OnlySinceTime(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")
	g.AddFriend(ctx, aliceID, bobID)

	g.SendMessage(ctx, aliceID, bobID, 1, "old msg")
	midpoint := time.Now()
	time.Sleep(10 * time.Millisecond)
	g.SendMessage(ctx, aliceID, bobID, 1, "new msg")

	result, err := g.SyncMessages(ctx, bobID, midpoint, 50)
	if err != nil {
		t.Fatalf("SyncMessages failed: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Content != "new msg" {
		t.Fatalf("expected 'new msg', got %q", result.Messages[0].Content)
	}
}

func TestSyncMessages_Limit(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")
	g.AddFriend(ctx, aliceID, bobID)

	before := time.Now()
	for i := 0; i < 5; i++ {
		g.SendMessage(ctx, aliceID, bobID, 1, "msg")
	}

	result, err := g.SyncMessages(ctx, bobID, before, 2)
	if err != nil {
		t.Fatalf("SyncMessages failed: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
}

func TestSyncAck(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")

	ackTime := time.Now().Truncate(time.Millisecond)
	err := g.SyncAck(ctx, aliceID, ackTime)
	if err != nil {
		t.Fatalf("SyncAck failed: %v", err)
	}

	got, err := g.dao.GetLastAckAt(ctx, aliceID)
	if err != nil {
		t.Fatalf("GetLastAckAt failed: %v", err)
	}
	if !got.Truncate(time.Millisecond).Equal(ackTime) {
		t.Fatalf("expected last_ack_at=%v, got %v", ackTime, got)
	}
}
