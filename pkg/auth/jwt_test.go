package auth

import (
	"fmt"
	"testing"
)

var secret = "secret-string"
var mid int64 = 123

func TestGenerateAndParse(t *testing.T) {
	jwt, err := GenerateToken(secret, mid, 9)
	if err != nil {
		t.Errorf("GenerateToken() failed: %v", err)
	}

	fmt.Println(jwt)

	claims, err := ParseToken(secret, jwt)
	if err != nil {
		t.Errorf("ParseToken() failed: %v", err)
	}

	fmt.Println(claims.Mid)
	if claims.Mid != mid {
		t.Errorf("claims.Mid = %d, want %d", claims.Mid, mid)
	}

	fmt.Println(claims.RegisteredClaims.IssuedAt)
	fmt.Println(claims.RegisteredClaims.ExpiresAt)
}
