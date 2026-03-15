package gateway

import (
	"context"
	"testing"
)

func TestAddFriend(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")

	aliceResp, _ := g.Login(ctx, "alice", "pw")
	bobResp, _ := g.Login(ctx, "bob", "pw")
	aliceID := aliceResp.(*LoginResponse).ID
	bobID := bobResp.(*LoginResponse).ID

	if err := g.AddFriend(ctx, aliceID, bobID); err != nil {
		t.Fatalf("AddFriend failed: %v", err)
	}

	// Verify bidirectional
	friends, err := g.ListFriend(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListFriend failed: %v", err)
	}
	if len(friends) != 1 || friends[0].ID != bobID {
		t.Fatalf("expected [bob], got %v", friends)
	}

	friends, err = g.ListFriend(ctx, bobID)
	if err != nil {
		t.Fatalf("ListFriend failed: %v", err)
	}
	if len(friends) != 1 || friends[0].ID != aliceID {
		t.Fatalf("expected [alice], got %v", friends)
	}
}

func TestAddFriend_Idempotent(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceResp, _ := g.Login(ctx, "alice", "pw")
	bobResp, _ := g.Login(ctx, "bob", "pw")
	aliceID := aliceResp.(*LoginResponse).ID
	bobID := bobResp.(*LoginResponse).ID

	g.AddFriend(ctx, aliceID, bobID)
	if err := g.AddFriend(ctx, aliceID, bobID); err != nil {
		t.Fatalf("expected idempotent, got: %v", err)
	}

	friends, _ := g.ListFriend(ctx, aliceID)
	if len(friends) != 1 {
		t.Fatalf("expected 1 friend after duplicate add, got %d", len(friends))
	}
}

func TestAddFriend_Self(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceResp, _ := g.Login(ctx, "alice", "pw")
	aliceID := aliceResp.(*LoginResponse).ID

	err := g.AddFriend(ctx, aliceID, aliceID)
	if err != ErrFriendSelf {
		t.Fatalf("expected ErrFriendSelf, got: %v", err)
	}
}

func TestAddFriend_UserNotFound(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceResp, _ := g.Login(ctx, "alice", "pw")
	aliceID := aliceResp.(*LoginResponse).ID

	err := g.AddFriend(ctx, aliceID, 99999)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got: %v", err)
	}
}

func TestRemoveFriend(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceResp, _ := g.Login(ctx, "alice", "pw")
	bobResp, _ := g.Login(ctx, "bob", "pw")
	aliceID := aliceResp.(*LoginResponse).ID
	bobID := bobResp.(*LoginResponse).ID

	g.AddFriend(ctx, aliceID, bobID)

	if err := g.RemoveFriend(ctx, aliceID, bobID); err != nil {
		t.Fatalf("RemoveFriend failed: %v", err)
	}

	// Verify bidirectional removal
	friends, _ := g.ListFriend(ctx, aliceID)
	if len(friends) != 0 {
		t.Fatalf("expected 0 friends for alice, got %d", len(friends))
	}
	friends, _ = g.ListFriend(ctx, bobID)
	if len(friends) != 0 {
		t.Fatalf("expected 0 friends for bob, got %d", len(friends))
	}
}

func TestRemoveFriend_NotFriend(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceResp, _ := g.Login(ctx, "alice", "pw")
	bobResp, _ := g.Login(ctx, "bob", "pw")
	aliceID := aliceResp.(*LoginResponse).ID
	bobID := bobResp.(*LoginResponse).ID

	err := g.RemoveFriend(ctx, aliceID, bobID)
	if err != ErrNotFriend {
		t.Fatalf("expected ErrNotFriend, got: %v", err)
	}
}

func TestListFriend_Empty(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceResp, _ := g.Login(ctx, "alice", "pw")
	aliceID := aliceResp.(*LoginResponse).ID

	friends, err := g.ListFriend(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListFriend failed: %v", err)
	}
	if len(friends) != 0 {
		t.Fatalf("expected 0 friends, got %d", len(friends))
	}
}
