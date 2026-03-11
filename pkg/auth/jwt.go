package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Claims is the JWT payload shared between API service (generation) and Logic service (validation).
type Claims struct {
	Mid int64 `json:"mid"`
	jwt.RegisteredClaims
}

// GenerateToken creates a signed JWT containing the user's mid.
func GenerateToken(secret string, mid int64, expireHours int) (string, error) {
	claims := Claims{
		Mid: mid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken validates a JWT string and returns the claims.
func ParseToken(secret string, tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	return token.Claims.(*Claims), nil
}
