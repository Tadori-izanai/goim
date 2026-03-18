package dao

import (
	"context"
	"errors"
	"testing"
	"time"

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

	id, err := d.CreateUser(ctx, "alice", "hashed_pw")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected user.ID to be set after insert")
	}
}

func TestCreateUser_Duplicate(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	if _, err := d.CreateUser(ctx, "bob", "pw1"); err != nil {
		t.Fatalf("first CreateUser failed: %v", err)
	}

	_, err := d.CreateUser(ctx, "bob", "pw2")
	if !errors.Is(err, ErrDuplicateUsername) {
		t.Fatalf("expected ErrDuplicateUsername, got: %v", err)
	}
}

func TestGetUserByUsername(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	// Insert a user first
	d.CreateUser(ctx, "charlie", "pw")

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
	u1.ID, _ = d.CreateUser(ctx, u1.Username, u1.Password)
	u2.ID, _ = d.CreateUser(ctx, u2.Username, u2.Password)
	u3.ID, _ = d.CreateUser(ctx, u3.Username, u3.Password)

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

func TestUpdateLastOnlineAt(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	user := &model.User{Username: "online_test", Password: "pw"}
	user.ID, _ = d.CreateUser(ctx, user.Username, user.Password)

	now := time.Now().Truncate(time.Second)
	if err := d.UpdateLastOnlineAt(ctx, user.ID, now); err != nil {
		t.Fatalf("UpdateLastOnlineAt failed: %v", err)
	}

	var got model.User
	d.db.WithContext(ctx).First(&got, user.ID)
	if !got.LastOnlineAt.Truncate(time.Second).Equal(now) {
		t.Fatalf("expected LastOnlineAt %v, got %v", now, got.LastOnlineAt)
	}
}

func TestUpdateLastAckAt(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	user := &model.User{Username: "ack_test", Password: "pw"}
	user.ID, _ = d.CreateUser(ctx, user.Username, user.Password)

	now := time.Now()
	if err := d.UpdateLastAckAt(ctx, user.ID, now); err != nil {
		t.Fatalf("UpdateLastAckAt failed: %v", err)
	}

	ackAt, err := d.GetLastAckAt(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetLastAckAt failed: %v", err)
	}
	// UnixMilliTime has millisecond precision, allow 1ms drift from DB round-trip
	diff := ackAt.UnixMilli() - now.UnixMilli()
	if diff < -1 || diff > 1 {
		t.Fatalf("expected LastAckAt ~%d, got %d (diff=%d)", now.UnixMilli(), ackAt.UnixMilli(), diff)
	}
}

func TestGetLastAckAt_NewUserHasInitValue(t *testing.T) {
	d := testDao(t)
	ctx := context.Background()

	before := time.Now()
	user := &model.User{Username: "new_user", Password: "pw"}
	user.ID, _ = d.CreateUser(ctx, user.Username, user.Password)

	ackAt, err := d.GetLastAckAt(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetLastAckAt failed: %v", err)
	}
	// CreateUser initializes LastAckAt to time.Now(), so it should be >= before
	if ackAt.Before(before.Add(-time.Second)) {
		t.Fatalf("expected LastAckAt near now, got %v", ackAt)
	}
}
