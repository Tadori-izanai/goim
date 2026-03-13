package dao

import (
	"context"
	"testing"
)

func TestAddFriend(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM friends")
	ctx := context.Background()

	if err := d.AddFriend(ctx, 1, 2); err != nil {
		t.Fatalf("AddFriend failed: %v", err)
	}

	// Verify bidirectional
	ok1, _ := d.IsFriend(ctx, 1, 2)
	ok2, _ := d.IsFriend(ctx, 2, 1)
	if !ok1 || !ok2 {
		t.Fatal("expected bidirectional friend relationship")
	}
}

func TestAddFriend_Idempotent(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM friends")
	ctx := context.Background()

	d.AddFriend(ctx, 1, 2)
	// Add again — should not error
	if err := d.AddFriend(ctx, 1, 2); err != nil {
		t.Fatalf("second AddFriend should be idempotent, got: %v", err)
	}
}

func TestRemoveFriend(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM friends")
	ctx := context.Background()

	d.AddFriend(ctx, 1, 2)
	if err := d.RemoveFriend(ctx, 1, 2); err != nil {
		t.Fatalf("RemoveFriend failed: %v", err)
	}

	ok1, _ := d.IsFriend(ctx, 1, 2)
	ok2, _ := d.IsFriend(ctx, 2, 1)
	if ok1 || ok2 {
		t.Fatal("expected both directions removed")
	}
}

func TestIsFriend_NotFriend(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM friends")
	ctx := context.Background()

	ok, err := d.IsFriend(ctx, 99, 100)
	if err != nil {
		t.Fatalf("IsFriend failed: %v", err)
	}
	if ok {
		t.Fatal("expected not friends")
	}
}

func TestListFriends(t *testing.T) {
	d := testDao(t)
	d.db.Exec("DELETE FROM friends")
	ctx := context.Background()

	d.AddFriend(ctx, 1, 2)
	d.AddFriend(ctx, 1, 3)

	ids, err := d.ListFriends(ctx, 1)
	if err != nil {
		t.Fatalf("ListFriends failed: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 friends, got %d", len(ids))
	}
}
