package gateway

import (
	"context"
	"testing"
)

func TestCreateGroup(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")

	group, err := g.CreateGroup(ctx, aliceID, "test-group")
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	if group.ID == 0 {
		t.Fatal("expected non-zero group ID")
	}
	if group.Name != "test-group" {
		t.Fatalf("expected name 'test-group', got %q", group.Name)
	}
	if group.OwnerID != aliceID {
		t.Fatalf("expected owner %d, got %d", aliceID, group.OwnerID)
	}
}

func TestJoinGroup(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")

	if err := g.JoinGroup(ctx, group.ID, bobID); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	members, err := g.ListGroupMembers(ctx, group.ID, aliceID)
	if err != nil {
		t.Fatalf("ListGroupMembers failed: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(members))
	}
}

func TestJoinGroup_Idempotent(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")
	g.JoinGroup(ctx, group.ID, bobID)

	if err := g.JoinGroup(ctx, group.ID, bobID); err != nil {
		t.Fatalf("expected idempotent, got: %v", err)
	}

	members, _ := g.ListGroupMembers(ctx, group.ID, aliceID)
	if len(members) != 2 {
		t.Fatalf("expected 2 members after duplicate join, got %d", len(members))
	}
}

func TestJoinGroup_NotFound(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")

	err := g.JoinGroup(ctx, 99999, aliceID)
	if err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound, got: %v", err)
	}
}

func TestQuitGroup(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")
	g.JoinGroup(ctx, group.ID, bobID)

	if err := g.QuitGroup(ctx, group.ID, bobID); err != nil {
		t.Fatalf("QuitGroup failed: %v", err)
	}

	members, _ := g.ListGroupMembers(ctx, group.ID, aliceID)
	if len(members) != 1 {
		t.Fatalf("expected 1 member after quit, got %d", len(members))
	}
}

func TestQuitGroup_NotMember(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")

	err := g.QuitGroup(ctx, group.ID, bobID)
	if err != ErrNotGroupMember {
		t.Fatalf("expected ErrNotGroupMember, got: %v", err)
	}
}

func TestQuitGroup_NotFound(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")

	err := g.QuitGroup(ctx, 99999, aliceID)
	if err != ErrGroupNotFound {
		t.Fatalf("expected ErrGroupNotFound, got: %v", err)
	}
}

func TestListGroupMembers_NotMember(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	group, _ := g.CreateGroup(ctx, aliceID, "test-group")

	_, err := g.ListGroupMembers(ctx, group.ID, bobID)
	if err != ErrNotGroupMember {
		t.Fatalf("expected ErrNotGroupMember, got: %v", err)
	}
}

func TestListJoinedGroups(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	g.Register(ctx, "bob", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")
	bobID := g.mustLoginID(t, ctx, "bob", "pw")

	g1, _ := g.CreateGroup(ctx, aliceID, "group-1")
	g2, _ := g.CreateGroup(ctx, aliceID, "group-2")
	g.JoinGroup(ctx, g1.ID, bobID)
	g.JoinGroup(ctx, g2.ID, bobID)

	groups, err := g.ListJoinedGroups(ctx, bobID)
	if err != nil {
		t.Fatalf("ListJoinedGroups failed: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestListJoinedGroups_Empty(t *testing.T) {
	g, _ := testGateway(t)
	ctx := context.Background()

	g.Register(ctx, "alice", "pw")
	aliceID := g.mustLoginID(t, ctx, "alice", "pw")

	groups, err := g.ListJoinedGroups(ctx, aliceID)
	if err != nil {
		t.Fatalf("ListJoinedGroups failed: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}
}
