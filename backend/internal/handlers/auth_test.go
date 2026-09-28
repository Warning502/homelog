package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/sgiraz/homelog/internal/middleware"
	"github.com/sgiraz/homelog/internal/models"
	"github.com/sgiraz/homelog/internal/testutil"
)

func init() { gin.SetMode(gin.TestMode) }

// seedUser writes a user with a known password into the test DB.
func seedUser(t *testing.T, h *AuthHandler, email, password string) *models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u := &models.User{
		Email:        email,
		PasswordHash: string(hash),
		Name:         "Test",
		Role:         "user",
		IsActive:     true,
	}
	if err := h.db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u
}

func newAuthTestRouter(t *testing.T) (*gin.Engine, *AuthHandler) {
	t.Helper()
	t.Setenv("JWT_SECRET", testutil.TestJWTSecret)
	db := testutil.NewDB(t)
	h := NewAuthHandler(db)
	r := gin.New()
	r.POST("/auth/forgot-password", h.ForgotPassword)
	r.POST("/auth/login", h.Login)
	return r, h
}

func postJSON(t *testing.T, r http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestForgotPassword_DoesNotLeakToken is the regression test for the Phase A
// security fix: the reset token must never appear in the JSON response by
// default. A leaked token lets any caller who knows a registered email reset
// that account's password.
func TestForgotPassword_DoesNotLeakToken(t *testing.T) {
	r, h := newAuthTestRouter(t)
	seedUser(t, h, "alice@example.com", "hunter2ABC")

	rec := postJSON(t, r, "/auth/forgot-password", map[string]string{"email": "alice@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, leaked := resp["reset_token"]; leaked {
		t.Fatalf("reset_token present in response: %v", resp)
	}
}

// When DEV_EXPOSE_RESET_TOKEN=true the token is expected in the response to
// support local development without SMTP.
func TestForgotPassword_DevGateReturnsToken(t *testing.T) {
	r, h := newAuthTestRouter(t)
	seedUser(t, h, "bob@example.com", "hunter2ABC")
	t.Setenv("DEV_EXPOSE_RESET_TOKEN", "true")

	rec := postJSON(t, r, "/auth/forgot-password", map[string]string{"email": "bob@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if tok, ok := resp["reset_token"].(string); !ok || tok == "" {
		t.Fatalf("expected reset_token in dev mode, got %v", resp)
	}
}

// Unknown email must not produce a different response than a known one (email
// enumeration protection). Both cases must also omit the token.
func TestForgotPassword_UnknownEmailGenericResponse(t *testing.T) {
	r, _ := newAuthTestRouter(t)

	rec := postJSON(t, r, "/auth/forgot-password", map[string]string{"email": "nobody@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, ok := resp["reset_token"]; ok {
		t.Fatalf("reset_token must not be present for unknown email: %v", resp)
	}
	if _, ok := resp["message"]; !ok {
		t.Fatalf("expected generic message, got %v", resp)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r, h := newAuthTestRouter(t)
	seedUser(t, h, "carol@example.com", "correct-horse")

	rec := postJSON(t, r, "/auth/login", map[string]string{
		"email":    "carol@example.com",
		"password": "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// A new account must start in the language the signup form was rendered in,
// and must fall back to the app default when the client sends nothing or
// something unsupported.
func TestRegister_InheritsBrowserLanguage(t *testing.T) {
	cases := []struct {
		name  string
		sent  any
		want  string
		email string
	}{
		{name: "regional tag", sent: "en-GB", want: "en", email: "en@example.com"},
		{name: "plain tag", sent: "it", want: "it", email: "it@example.com"},
		{name: "unsupported", sent: "ja", want: models.DefaultLanguage, email: "ja@example.com"},
		{name: "omitted", sent: nil, want: models.DefaultLanguage, email: "none@example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JWT_SECRET", testutil.TestJWTSecret)
			db := testutil.NewDB(t)
			h := NewAuthHandler(db)
			r := gin.New()
			r.POST("/auth/register", h.Register)

			body := map[string]any{"email": tc.email, "password": "secret123", "name": "Test"}
			if tc.sent != nil {
				body["language"] = tc.sent
			}
			if rec := postJSON(t, r, "/auth/register", body); rec.Code != http.StatusCreated {
				t.Fatalf("register: status %d, body %s", rec.Code, rec.Body.String())
			}

			var user models.User
			if err := db.Where("email = ?", tc.email).First(&user).Error; err != nil {
				t.Fatalf("user not created: %v", err)
			}
			var settings models.UserSettings
			if err := db.Where("user_id = ?", user.ID).First(&settings).Error; err != nil {
				t.Fatalf("settings not created: %v", err)
			}
			if settings.Language != tc.want {
				t.Errorf("language = %q, want %q", settings.Language, tc.want)
			}
		})
	}
}

// newSessionTestRouter wires the auth endpoints that issue, rotate and revoke
// sessions, plus one protected route to prove an access token works.
func newSessionTestRouter(t *testing.T) (*gin.Engine, *AuthHandler) {
	t.Helper()
	r, h := newAuthTestRouter(t)
	r.POST("/auth/refresh", h.RefreshToken)
	r.POST("/auth/reset-password", h.ResetPassword)
	r.POST("/auth/logout", h.Logout)
	p := r.Group("")
	p.Use(middleware.AuthRequired())
	p.PUT("/settings/password", h.ChangePassword)
	p.GET("/me", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r, h
}

func refreshCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == refreshCookieName {
			return ck
		}
	}
	return nil
}

func postWithCookie(t *testing.T, r http.Handler, path string, ck *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	if ck != nil {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func login(t *testing.T, r http.Handler, email, password string) (string, *http.Cookie) {
	t.Helper()
	rec := postJSON(t, r, "/auth/login", map[string]string{"email": email, "password": password})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, leaked := resp["refresh_token"]; leaked {
		t.Fatalf("refresh_token must not be in the response body: %v", resp)
	}
	ck := refreshCookie(t, rec)
	if ck == nil || ck.Value == "" {
		t.Fatal("login did not set the refresh cookie")
	}
	if !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode {
		t.Fatalf("refresh cookie must be HttpOnly+SameSite=Strict, got %+v", ck)
	}
	return resp["token"].(string), ck
}

func TestRefresh_WithCookieRotatesSession(t *testing.T) {
	r, h := newSessionTestRouter(t)
	seedUser(t, h, "carol@example.com", "correct-horse")
	_, ck := login(t, r, "carol@example.com", "correct-horse")

	rec := postWithCookie(t, r, "/auth/refresh", ck)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d: %s", rec.Code, rec.Body.String())
	}
	if refreshCookie(t, rec) == nil {
		t.Fatal("refresh did not rotate the cookie")
	}
}

func TestRefresh_RejectsAccessToken(t *testing.T) {
	r, h := newSessionTestRouter(t)
	seedUser(t, h, "dave@example.com", "correct-horse")
	access, _ := login(t, r, "dave@example.com", "correct-horse")

	rec := postJSON(t, r, "/auth/refresh", map[string]string{"refresh_token": access})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("access token accepted by /auth/refresh: status = %d", rec.Code)
	}
}

func TestRefresh_RejectedAsBearer(t *testing.T) {
	r, h := newSessionTestRouter(t)
	seedUser(t, h, "erin@example.com", "correct-horse")
	_, ck := login(t, r, "erin@example.com", "correct-horse")

	rec := doJSON(t, r, http.MethodGet, "/me", ck.Value, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh token accepted as bearer: status = %d", rec.Code)
	}
}

func TestChangePassword_RevokesOtherSessions(t *testing.T) {
	r, h := newSessionTestRouter(t)
	seedUser(t, h, "frank@example.com", "correct-horse")
	_, otherDevice := login(t, r, "frank@example.com", "correct-horse")
	access, _ := login(t, r, "frank@example.com", "correct-horse")

	rec := doJSON(t, r, http.MethodPut, "/settings/password", access,
		map[string]string{"current_password": "correct-horse", "new_password": "battery-staple"})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password status = %d: %s", rec.Code, rec.Body.String())
	}
	fresh := refreshCookie(t, rec)
	if fresh == nil {
		t.Fatal("change password must hand the caller a fresh session")
	}

	if rec := postWithCookie(t, r, "/auth/refresh", otherDevice); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old refresh token still works after password change: status = %d", rec.Code)
	}
	if rec := postWithCookie(t, r, "/auth/refresh", fresh); rec.Code != http.StatusOK {
		t.Fatalf("caller's new session rejected: status = %d", rec.Code)
	}
}

func TestResetPassword_RevokesSessions(t *testing.T) {
	r, h := newSessionTestRouter(t)
	u := seedUser(t, h, "gina@example.com", "correct-horse")
	_, ck := login(t, r, "gina@example.com", "correct-horse")

	t.Setenv("DEV_EXPOSE_RESET_TOKEN", "true")
	rec := postJSON(t, r, "/auth/forgot-password", map[string]string{"email": u.Email})
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	resetTok, _ := resp["reset_token"].(string)

	rec = postJSON(t, r, "/auth/reset-password", map[string]string{"token": resetTok, "new_password": "battery-staple"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := postWithCookie(t, r, "/auth/refresh", ck); rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh token survived a password reset: status = %d", rec.Code)
	}
}

func TestPasswordMinimumLength(t *testing.T) {
	r, h := newSessionTestRouter(t)
	seedUser(t, h, "hank@example.com", "correct-horse")
	access, _ := login(t, r, "hank@example.com", "correct-horse")

	rec := doJSON(t, r, http.MethodPut, "/settings/password", access,
		map[string]string{"current_password": "correct-horse", "new_password": "1234567"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("7-char password accepted: status = %d", rec.Code)
	}
}

func TestLogout_ClearsCookie(t *testing.T) {
	r, _ := newSessionTestRouter(t)
	rec := postWithCookie(t, r, "/auth/logout", nil)
	ck := refreshCookie(t, rec)
	if ck == nil || ck.MaxAge >= 0 {
		t.Fatalf("logout must expire the refresh cookie, got %+v", ck)
	}
}
