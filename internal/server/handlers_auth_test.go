package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/config"
)

// doJSON helper: encodes body, executes, returns recorder.
func doJSON(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	buf := new(bytes.Buffer)
	if body != nil {
		require.NoError(t, json.NewEncoder(buf).Encode(body))
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, buf)
	req.RemoteAddr = "10.0.0.99:1234"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestSessionStatus_OpenByDefault(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	w := doJSON(t, h, http.MethodGet, "/api/v1/session", nil, nil)
	require.Equal(t, http.StatusOK, w.Code)
	var st map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
	require.Equal(t, false, st["auth_enabled"])
	require.Equal(t, false, st["authenticated"])
}

func TestAuthEnable_AutoGeneratesPassword(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	w := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin"}, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "admin", body["username"])
	require.GreaterOrEqual(t, len(body["password"]), 24, "auto password should be returned once")
	require.NotEmpty(t, w.Result().Cookies(), "enable should issue a session cookie")

	// auth_enabled is true now
	w2 := doJSON(t, h, http.MethodGet, "/api/v1/session", nil, nil)
	var st map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &st))
	require.Equal(t, true, st["auth_enabled"])
}

func TestAuthEnable_RejectedWhenAlreadyEnabled(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	w1 := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin"}, nil)
	require.Equal(t, http.StatusOK, w1.Code)

	w2 := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin2"}, nil)
	require.Equal(t, http.StatusConflict, w2.Code)
}

func TestSessionLogin_HappyPath(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	// Enable auth with explicit password.
	enable := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)
	require.Equal(t, http.StatusOK, enable.Code, enable.Body.String())

	// Wrong password rejected.
	bad := doJSON(t, h, http.MethodPost, "/api/v1/session",
		map[string]string{"username": "admin", "password": "wrong"}, nil)
	require.Equal(t, http.StatusUnauthorized, bad.Code)

	// Right password sets cookie.
	good := doJSON(t, h, http.MethodPost, "/api/v1/session",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)
	require.Equal(t, http.StatusOK, good.Code, good.Body.String())
	require.NotEmpty(t, good.Result().Cookies())
}

func TestAuth_CookieGatesAPI(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	enable := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)
	require.Equal(t, http.StatusOK, enable.Code)
	cookies := enable.Result().Cookies()
	require.NotEmpty(t, cookies)

	// /api/v1/summary without cookie → 401
	w1 := doJSON(t, h, http.MethodGet, "/api/v1/summary", nil, nil)
	require.Equal(t, http.StatusUnauthorized, w1.Code)

	// With cookie → 200
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/summary", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
}

func TestAuth_APIKeyAlsoGates(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	_ = doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)

	// Programmatic client uses X-API-Key — bypasses cookie.
	w := doJSON(t, h, http.MethodGet, "/api/v1/summary", nil,
		map[string]string{"X-API-Key": testAPIKey})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Wrong key → 401.
	w2 := doJSON(t, h, http.MethodGet, "/api/v1/summary", nil,
		map[string]string{"X-API-Key": "wrong"})
	require.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestSessionLogout_RevokesCookie(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	enable := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)
	cookies := enable.Result().Cookies()

	// Logout.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/api/v1/session", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Subsequent request with the same cookie → 401.
	req2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/summary", nil)
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestAuthDisable_RequiresPasswordAndSession(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	enable := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)
	cookies := enable.Result().Cookies()

	// Disable with wrong password → 401.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/disable",
		bytes.NewBufferString(`{"password":"wrong"}`))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// Disable with X-API-Key only (no cookie) → forbidden.
	req2 := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/disable",
		bytes.NewBufferString(`{"password":"supersecret123"}`))
	req2.Header.Set("X-API-Key", testAPIKey)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusForbidden, w2.Code)

	// Disable with valid cookie + password → ok, auth becomes disabled.
	req3 := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/disable",
		bytes.NewBufferString(`{"password":"supersecret123"}`))
	for _, c := range cookies {
		req3.AddCookie(c)
	}
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code, w3.Body.String())

	// /api/v1/summary now open again.
	w4 := doJSON(t, h, http.MethodGet, "/api/v1/summary", nil, nil)
	require.Equal(t, http.StatusOK, w4.Code)
}

func TestAuthChangePassword_RotatesAndRejectsOld(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	enable := doJSON(t, h, http.MethodPost, "/api/v1/auth/enable",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)
	cookies := enable.Result().Cookies()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/password",
		bytes.NewBufferString(`{"current":"supersecret123","new":"newsupersecret456"}`))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Logout, then old password no longer works.
	bad := doJSON(t, h, http.MethodPost, "/api/v1/session",
		map[string]string{"username": "admin", "password": "supersecret123"}, nil)
	require.Equal(t, http.StatusUnauthorized, bad.Code)

	good := doJSON(t, h, http.MethodPost, "/api/v1/session",
		map[string]string{"username": "admin", "password": "newsupersecret456"}, nil)
	require.Equal(t, http.StatusOK, good.Code)
}

func TestAuthRateLimit_Sessions(t *testing.T) {
	cfg := &config.Config{HTTP: config.HTTPConfig{Bind: "127.0.0.1:9494"}}
	h := buildSrvM6(t, cfg)

	// Enable from a different IP so the auth-rate bucket for the test IP starts full.
	enable := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/enable",
		bytes.NewBufferString(`{"username":"admin","password":"supersecret123"}`))
	enable.RemoteAddr = "10.0.0.1:1234"
	wE := httptest.NewRecorder()
	h.ServeHTTP(wE, enable)
	require.Equal(t, http.StatusOK, wE.Code, wE.Body.String())

	// From a fresh IP: 5 wrong-password attempts allowed (burst), 6th throttled.
	for i := 0; i < 5; i++ {
		w := doJSON(t, h, http.MethodPost, "/api/v1/session",
			map[string]string{"username": "admin", "password": "wrong"}, nil)
		require.Equal(t, http.StatusUnauthorized, w.Code, "attempt %d", i+1)
	}
	w := doJSON(t, h, http.MethodPost, "/api/v1/session",
		map[string]string{"username": "admin", "password": "wrong"}, nil)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
}
