package comet

import (
	"encoding/json"
	"fmt"

	"github.com/Terry-Mao/goim/pkg/auth"
)

// processAuthBody handles both legacy and JWT auth body formats.
// Legacy: {"mid":123, "key":"", "room_id":"...", "platform":"web", "accepts":[...]}
// JWT:    {"token":"eyJ...", "room_id":"...", "platform":"web", "accepts":[...]}
//
// If a JWT token is present, it is validated and the body is reconstructed
// into the legacy format so that Logic.Connect() needs zero changes.
func processAuthBody(jwtSecret string, body []byte) ([]byte, error) {
	var req struct {
		Token    string  `json:"token"`
		Mid      int64   `json:"mid"`
		Key      string  `json:"key"`
		RoomID   string  `json:"room_id"`
		Platform string  `json:"platform"`
		Accepts  []int32 `json:"accepts"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return body, nil // can't parse, let Logic handle it
	}
	if req.Token == "" {
		// Legacy format — pass through unchanged
		return body, nil
	}
	// JWT format — validate token, extract mid, reconstruct body
	claims, err := auth.ParseToken(jwtSecret, req.Token)
	if err != nil {
		return nil, fmt.Errorf("jwt auth failed: %w", err)
	}
	rebuilt := struct {
		Mid      int64   `json:"mid"`
		Key      string  `json:"key"`
		RoomID   string  `json:"room_id"`
		Platform string  `json:"platform"`
		Accepts  []int32 `json:"accepts"`
	}{
		Mid:      claims.Mid,
		Key:      req.Key,
		RoomID:   req.RoomID,
		Platform: req.Platform,
		Accepts:  req.Accepts,
	}
	return json.Marshal(rebuilt)
}
