package dao

import (
	"context"
	"testing"
	"time"

	"github.com/Terry-Mao/goim/internal/gateway/model"
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

func TestCreateGroupMessage(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	msgID, err := d.CreateGroupMessage(ctx, 1, 100, 1, "hello group")
	if err != nil {
		t.Fatalf("CreateGroupMessage failed: %v", err)
	}
	if msgID == "" {
		t.Fatal("expected non-empty msg_id")
	}
}

func TestListGroupMessagesSince(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	before := time.Now().Add(-time.Second)

	d.CreateGroupMessage(ctx, 1, 100, 1, "msg1")
	d.CreateGroupMessage(ctx, 1, 200, 1, "msg2")
	d.CreateGroupMessage(ctx, 1, 100, 1, "msg3")

	msgs, err := d.ListGroupMessagesSince(ctx, 1, before, 50)
	if err != nil {
		t.Fatalf("ListGroupMessagesSince failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "msg1" || msgs[2].Content != "msg3" {
		t.Fatalf("expected ASC order, got: %s, %s, %s", msgs[0].Content, msgs[1].Content, msgs[2].Content)
	}
}

func TestListGroupMessagesSince_OnlyTargetGroup(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	before := time.Now().Add(-time.Second)

	d.CreateGroupMessage(ctx, 1, 100, 1, "group 1 msg")
	d.CreateGroupMessage(ctx, 2, 100, 1, "group 2 msg")

	msgs, err := d.ListGroupMessagesSince(ctx, 1, before, 50)
	if err != nil {
		t.Fatalf("ListGroupMessagesSince failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for group 1, got %d", len(msgs))
	}
}

func TestListGroupMessagesSince_Limit(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	before := time.Now().Add(-time.Second)
	for i := 0; i < 5; i++ {
		d.CreateGroupMessage(ctx, 1, 100, 1, "msg")
	}

	msgs, err := d.ListGroupMessagesSince(ctx, 1, before, 3)
	if err != nil {
		t.Fatalf("ListGroupMessagesSince failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages with limit, got %d", len(msgs))
	}
}

func TestListOfflineGroupMessages(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	// Create user and two groups
	u := &model.User{Username: "offline_user", Password: "pw"}
	u.ID, _ = d.CreateUser(ctx, "offline_user", "pw")

	g1, _ := d.CreateGroup(ctx, "group1", u.ID)
	g2, _ := d.CreateGroup(ctx, "group2", u.ID)

	// Create a third group that user is NOT in
	otherUser := &model.User{Username: "other", Password: "pw"}
	otherUser.ID, _ = d.CreateUser(ctx, "other", "pw")
	g3, _ := d.CreateGroup(ctx, "group3", otherUser.ID)

	time.Sleep(10 * time.Millisecond)
	since := time.Now()
	time.Sleep(10 * time.Millisecond)

	// Messages after since in groups user belongs to
	d.CreateGroupMessage(ctx, g1.ID, otherUser.ID, 1, "g1 msg1")
	d.CreateGroupMessage(ctx, g2.ID, otherUser.ID, 1, "g2 msg1")
	// Message in group user is NOT in — should not appear
	d.CreateGroupMessage(ctx, g3.ID, otherUser.ID, 1, "g3 msg1")

	msgs, err := d.ListOfflineGroupMessages(ctx, u.ID, since, 200)
	if err != nil {
		t.Fatalf("ListOfflineGroupMessages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 offline group messages, got %d", len(msgs))
	}
}

func TestListOfflineGroupMessages_FiltersBeforeJoin(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	owner := &model.User{Username: "owner2", Password: "pw"}
	owner.ID, _ = d.CreateUser(ctx, "owner2", "pw")
	g, _ := d.CreateGroup(ctx, "late_join_group", owner.ID)

	// Message sent BEFORE user joins
	d.CreateGroupMessage(ctx, g.ID, owner.ID, 1, "before join")

	time.Sleep(10 * time.Millisecond)

	// New user joins the group
	joiner := &model.User{Username: "joiner", Password: "pw"}
	joiner.ID, _ = d.CreateUser(ctx, "joiner", "pw")
	d.JoinGroup(ctx, g.ID, joiner.ID)

	time.Sleep(10 * time.Millisecond)

	// Message sent AFTER user joins
	d.CreateGroupMessage(ctx, g.ID, owner.ID, 1, "after join")

	// since=zero to get all, but should still filter pre-join messages
	msgs, err := d.ListOfflineGroupMessages(ctx, joiner.ID, time.Time{}, 200)
	if err != nil {
		t.Fatalf("ListOfflineGroupMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message (after join only), got %d", len(msgs))
	}
	if msgs[0].Content != "after join" {
		t.Fatalf("expected 'after join', got '%s'", msgs[0].Content)
	}
}

func TestListOfflineGroupMessages_Limit(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	u := &model.User{Username: "limit_user", Password: "pw"}
	u.ID, _ = d.CreateUser(ctx, "limit_user", "pw")
	g, _ := d.CreateGroup(ctx, "limit_group", u.ID)

	since := time.Now()
	time.Sleep(10 * time.Millisecond)

	for i := 0; i < 5; i++ {
		d.CreateGroupMessage(ctx, g.ID, u.ID, 1, "msg")
	}

	msgs, err := d.ListOfflineGroupMessages(ctx, u.ID, since, 3)
	if err != nil {
		t.Fatalf("ListOfflineGroupMessages failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages with limit, got %d", len(msgs))
	}
}
