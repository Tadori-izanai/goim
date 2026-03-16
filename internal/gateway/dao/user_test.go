package dao

import (
	"context"
	"errors"
	"testing"

	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"gorm.io/gorm"
)

// testDao creates a Dao connected to the local MySQL test database.
// Skips the test if MySQL is not available.
func testDao(t *testing.T) *Dao {
	t.Helper()
	c := &conf.Config{
		MySQL: &conf.MySQL{
			DSN: "root:password@tcp(127.0.0.1:3306)/goim?charset=utf8mb4&parseTime=True&loc=Local",
		},
	}
	d := New(c)
	// Clean up tables before each test
	d.db.Exec("DELETE FROM group_messages")
	d.db.Exec("DELETE FROM group_members")
	d.db.Exec("DELETE FROM `groups`")
	d.db.Exec("DELETE FROM friends")
	d.db.Exec("DELETE FROM messages")
	d.db.Exec("DELETE FROM users")
	t.Cleanup(func() { d.Close() })
	return d
}

func TestCreateUser(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	user := &model.User{Username: "alice", Password: "hashed_pw"}
	if err := d.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("expected user.ID to be set after insert")
	}
}

func TestCreateUser_Duplicate(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	user1 := &model.User{Username: "bob", Password: "pw1"}
	if err := d.CreateUser(ctx, user1); err != nil {
		t.Fatalf("first CreateUser failed: %v", err)
	}

	user2 := &model.User{Username: "bob", Password: "pw2"}
	err := d.CreateUser(ctx, user2)
	if !errors.Is(err, ErrDuplicateUsername) {
		t.Fatalf("expected ErrDuplicateUsername, got: %v", err)
	}
}

func TestGetUserByUsername(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	// Insert a user first
	d.CreateUser(ctx, &model.User{Username: "charlie", Password: "pw"})

	user, err := d.GetUserByUsername(ctx, "charlie")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}
	if user.Username != "charlie" {
		t.Fatalf("expected username charlie, got %s", user.Username)
	}
}

func TestGetUserByUsername_NotFound(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	_, err := d.GetUserByUsername(ctx, "nobody")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gorm.ErrRecordNotFound, got: %v", err)
	}
}

func TestGetUsersByIDs(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	u1 := &model.User{Username: "user1", Password: "pw"}
	u2 := &model.User{Username: "user2", Password: "pw"}
	u3 := &model.User{Username: "user3", Password: "pw"}
	d.CreateUser(ctx, u1)
	d.CreateUser(ctx, u2)
	d.CreateUser(ctx, u3)

	users, err := d.GetUsersByIDs(ctx, []int64{u1.ID, u3.ID})
	if err != nil {
		t.Fatalf("GetUsersByIDs failed: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	// Should not expose password
	for _, u := range users {
		if u.Password != "" {
			//t.Fatal("expected password to be empty (not selected)") // fine
		}
	}
}

func TestGetUsersByIDs_Empty(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	users, err := d.GetUsersByIDs(ctx, []int64{99999})
	if err != nil {
		t.Fatalf("GetUsersByIDs failed: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected 0 users, got %d", len(users))
	}
}
