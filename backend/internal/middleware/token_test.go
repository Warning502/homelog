package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-secret-for-token-tests"

func signClaims(t *testing.T, method jwt.SigningMethod, claims JWTClaims) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(method, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return tok
}

func claimsOfType(typ string) JWTClaims {
	return JWTClaims{
		UserID:    1,
		Email:     "a@example.com",
		Role:      "user",
		TokenType: typ,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
}

func bearerStatus(t *testing.T, token string) int {
	t.Helper()
	r := newPingRouter(AuthRequired())
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func TestAuthRequired_AcceptsAccessToken(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	tok := signClaims(t, jwt.SigningMethodHS256, claimsOfType(TokenTypeAccess))
	if got := bearerStatus(t, tok); got != http.StatusOK {
		t.Fatalf("access token: status = %d, want 200", got)
	}
}

// A refresh token lives 7 days; accepting it as a bearer token would make the
// 15-minute access-token lifetime meaningless.
func TestAuthRequired_RejectsRefreshToken(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	tok := signClaims(t, jwt.SigningMethodHS256, claimsOfType(TokenTypeRefresh))
	if got := bearerStatus(t, tok); got != http.StatusUnauthorized {
		t.Fatalf("refresh token as bearer: status = %d, want 401", got)
	}
}

// Tokens minted before the typ claim existed carry no type and must not pass.
func TestAuthRequired_RejectsUntypedToken(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	tok := signClaims(t, jwt.SigningMethodHS256, claimsOfType(""))
	if got := bearerStatus(t, tok); got != http.StatusUnauthorized {
		t.Fatalf("untyped token: status = %d, want 401", got)
	}
}

func TestParseToken_RejectsOtherAlgorithms(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	tok := signClaims(t, jwt.SigningMethodHS512, claimsOfType(TokenTypeAccess))
	if _, err := ParseToken(tok, TokenTypeAccess); err == nil {
		t.Fatal("HS512 token accepted, want only HS256")
	}
}

func TestParseToken_RequiresExpiry(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	c := claimsOfType(TokenTypeAccess)
	c.ExpiresAt = nil
	tok := signClaims(t, jwt.SigningMethodHS256, c)
	if _, err := ParseToken(tok, TokenTypeAccess); err == nil {
		t.Fatal("token without exp accepted")
	}
}

func TestDemoGuard_ReturnsErrorCode(t *testing.T) {
	t.Setenv("DEMO_MODE", "true")
	r := newPingRouter(DemoGuard())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error_code"] != "demo_mode_forbidden" {
		t.Fatalf("error_code = %v, want demo_mode_forbidden", body["error_code"])
	}
}
