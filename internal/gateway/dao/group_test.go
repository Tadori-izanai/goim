package dao

import (
	"context"
	"testing"
)

func TestCreateGroup(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	group, err := d.CreateGroup(ctx, "test-group", 1)
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}
	groupID := group.ID
	if groupID == 0 {
		t.Fatal("expected group ID to be set")
	}

	// Owner should be a member
	ok, err := d.IsInGroup(ctx, groupID, 1)
	if err != nil {
		t.Fatalf("IsInGroup failed: %v", err)
	}
	if !ok {
		t.Fatal("expected owner to be a group member")
	}
}

func TestJoinGroup(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	group, _ := d.CreateGroup(ctx, "test-group", 1)
	groupID := group.ID

	if err := d.JoinGroup(ctx, groupID, 2); err != nil {
		t.Fatalf("JoinGroup failed: %v", err)
	}

	ok, _ := d.IsInGroup(ctx, groupID, 2)
	if !ok {
		t.Fatal("expected user 2 to be in group")
	}
}

func TestJoinGroup_Idempotent(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	group, _ := d.CreateGroup(ctx, "test-group", 1)
	groupID := group.ID
	d.JoinGroup(ctx, groupID, 2)

	// Join again — should not error
	if err := d.JoinGroup(ctx, groupID, 2); err != nil {
		t.Fatalf("second JoinGroup should be idempotent, got: %v", err)
	}
}

func TestQuitGroup(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	group, _ := d.CreateGroup(ctx, "test-group", 1)
	groupID := group.ID
	d.JoinGroup(ctx, groupID, 2)

	if err := d.QuitGroup(ctx, groupID, 2); err != nil {
		t.Fatalf("QuitGroup failed: %v", err)
	}

	ok, _ := d.IsInGroup(ctx, groupID, 2)
	if ok {
		t.Fatal("expected user 2 to not be in group after quit")
	}
}

func TestIsInGroup_NotMember(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	group, _ := d.CreateGroup(ctx, "test-group", 1)
	groupID := group.ID

	ok, err := d.IsInGroup(ctx, groupID, 99)
	if err != nil {
		t.Fatalf("IsInGroup failed: %v", err)
	}
	if ok {
		t.Fatal("expected user 99 to not be in group")
	}
}

func TestListMemberIDs(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	group, _ := d.CreateGroup(ctx, "test-group", 1)
	groupID := group.ID
	d.JoinGroup(ctx, groupID, 2)
	d.JoinGroup(ctx, groupID, 3)

	ids, err := d.ListMemberIDs(ctx, groupID)
	if err != nil {
		t.Fatalf("ListMemberIDs failed: %v", err)
	}
	if len(ids) != 3 { // owner + 2 joined
		t.Fatalf("expected 3 members, got %d", len(ids))
	}
}

func TestListGroupIDs(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	g1, _ := d.CreateGroup(ctx, "group-1", 1)
	g2, _ := d.CreateGroup(ctx, "group-2", 2)
	d.JoinGroup(ctx, g1.ID, 3)
	d.JoinGroup(ctx, g2.ID, 3)

	ids, err := d.ListGroupIDs(ctx, 3)
	if err != nil {
		t.Fatalf("ListGroupIDs failed: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(ids))
	}
}

func TestGetGroupByIDs(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	g1, _ := d.CreateGroup(ctx, "group-a", 1)
	d.CreateGroup(ctx, "group-b", 1)
	g3, _ := d.CreateGroup(ctx, "group-c", 1)

	groups, err := d.GetGroupByIDs(ctx, []int64{g1.ID, g3.ID})
	if err != nil {
		t.Fatalf("GetGroupByIDs failed: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}
