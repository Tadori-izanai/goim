package dao

import (
	"context"
	"testing"
	"time"
)

func TestCreateMessage(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM messages")
	ctx := context.Background()

	msgID, err := d.CreateMessage(ctx, 1, 2, 1, "hello")
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}
	if msgID == "" {
		t.Fatal("expected non-empty msg_id")
	}
}

func TestListMessagesSince(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM messages")
	ctx := context.Background()

	before := time.Now().Add(-time.Second)

	// Insert 3 messages to user 2
	d.CreateMessage(ctx, 1, 2, 1, "msg1")
	d.CreateMessage(ctx, 3, 2, 1, "msg2")
	d.CreateMessage(ctx, 1, 2, 1, "msg3")

	msgs, err := d.ListMessagesSince(ctx, 2, before, 50)
	if err != nil {
		t.Fatalf("ListMessagesSince failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	// Verify ASC order
	if msgs[0].Content != "msg1" || msgs[2].Content != "msg3" {
		t.Fatalf("expected ASC order, got: %s, %s, %s", msgs[0].Content, msgs[1].Content, msgs[2].Content)
	}
}

func TestListMessagesSince_OnlyAfterTime(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM messages")
	ctx := context.Background()

	// Old message
	d.CreateMessage(ctx, 1, 2, 1, "old")

	time.Sleep(10 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(10 * time.Millisecond)

	// New message
	d.CreateMessage(ctx, 1, 2, 1, "new")

	msgs, err := d.ListMessagesSince(ctx, 2, cutoff, 50)
	if err != nil {
		t.Fatalf("ListMessagesSince failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after cutoff, got %d", len(msgs))
	}
	if msgs[0].Content != "new" {
		t.Fatalf("expected 'new', got '%s'", msgs[0].Content)
	}
}

func TestListMessagesSince_OnlyTargetUser(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM messages")
	ctx := context.Background()

	before := time.Now().Add(-time.Second)

	d.CreateMessage(ctx, 1, 2, 1, "to user 2")
	d.CreateMessage(ctx, 1, 3, 1, "to user 3")

	msgs, err := d.ListMessagesSince(ctx, 2, before, 50)
	if err != nil {
		t.Fatalf("ListMessagesSince failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for user 2, got %d", len(msgs))
	}
}

func TestListMessagesSince_Limit(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM messages")
	ctx := context.Background()

	before := time.Now().Add(-time.Second)
	for i := 0; i < 5; i++ {
		d.CreateMessage(ctx, 1, 2, 1, "msg")
	}

	msgs, err := d.ListMessagesSince(ctx, 2, before, 3)
	if err != nil {
		t.Fatalf("ListMessagesSince failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages with limit, got %d", len(msgs))
	}
}
