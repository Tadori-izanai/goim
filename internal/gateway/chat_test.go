package gateway

import (
	"context"
	"testing"
	"time"
)

func TestSendMessage(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")
	g.AddFriend(ctx, aliceID, bobID)

	msgID, err := g.SendMessage(ctx, aliceID, bobID, 1, "hello bob")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	if msgID == "" {
		t.Fatal("expected non-empty msg_id")
	}
}

func TestSendMessage_NotFriend(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	_, err := g.SendMessage(ctx, aliceID, bobID, 1, "hello")
	if err != ErrNotFriend {
		t.Fatalf("expected ErrNotFriend, got: %v", err)
	}
}

func TestHistoryMessage(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")
	g.AddFriend(ctx, aliceID, bobID)

	before := time.Now()
	g.SendMessage(ctx, aliceID, bobID, 1, "msg1")
	g.SendMessage(ctx, aliceID, bobID, 1, "msg2")

	messages, err := g.HistoryMessage(ctx, bobID, before, 50)
	if err != nil {
		t.Fatalf("HistoryMessage failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Content != "msg1" || messages[1].Content != "msg2" {
		t.Fatalf("unexpected message order: %s, %s", messages[0].Content, messages[1].Content)
	}
}

func TestHistoryMessage_OnlyRecipient(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	g.Register(ctx, "charlie", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")
	charlieID := g.mustLoginID(t, ctx, "charlie", "pw")
	g.AddFriend(ctx, aliceID, bobID)

	before := time.Now()
	g.SendMessage(ctx, aliceID, bobID, 1, "for bob")

	// Charlie should see nothing
	messages, err := g.HistoryMessage(ctx, charlieID, before, 50)
	if err != nil {
		t.Fatalf("HistoryMessage failed: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages for charlie, got %d", len(messages))
	}
}

func TestHistoryMessage_Limit(t *testing.T) {
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

	messages, err := g.HistoryMessage(ctx, bobID, before, 3)
	if err != nil {
		t.Fatalf("HistoryMessage failed: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
}

func TestGroupMessage(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")
	g.JoinGroup(ctx, group.ID, bobID)

	msgID, err := g.SendGroupMessage(ctx, group.ID, aliceID, 1, "hello group")
	if err != nil {
		t.Fatalf("SendGroupMessage failed: %v", err)
	}
	if msgID == "" {
		t.Fatal("expected non-empty msg_id")
	}
}

func TestGroupMessage_NotMember(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")

	_, err := g.SendGroupMessage(ctx, group.ID, bobID, 1, "hello")
	if err != ErrNotGroupMember {
		t.Fatalf("expected ErrNotGroupMember, got: %v", err)
	}
}

func TestHistoryGroupMessage(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")
	g.JoinGroup(ctx, group.ID, bobID)

	before := time.Now()
	g.SendGroupMessage(ctx, group.ID, aliceID, 1, "msg1")
	g.SendGroupMessage(ctx, group.ID, bobID, 1, "msg2")

	messages, err := g.HistoryGroupMessage(ctx, group.ID, aliceID, before, 50)
	if err != nil {
		t.Fatalf("HistoryGroupMessage failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Content != "msg1" || messages[1].Content != "msg2" {
		t.Fatalf("unexpected order: %s, %s", messages[0].Content, messages[1].Content)
	}
}

func TestHistoryGroupMessage_NotMember(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")

	_, err := g.HistoryGroupMessage(ctx, group.ID, bobID, time.Now(), 50)
	if err != ErrNotGroupMember {
		t.Fatalf("expected ErrNotGroupMember, got: %v", err)
	}
}

func TestHistoryGroupMessage_Limit(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")

	before := time.Now()
	for i := 0; i < 5; i++ {
		g.SendGroupMessage(ctx, group.ID, aliceID, 1, "msg")
	}

	messages, err := g.HistoryGroupMessage(ctx, group.ID, aliceID, before, 3)
	if err != nil {
		t.Fatalf("HistoryGroupMessage failed: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
}
