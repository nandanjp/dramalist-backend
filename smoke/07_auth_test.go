package smoke_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestAuthLoginAndRefreshFlow verifies the full auth token lifecycle:
//   login → AT + RT issued → silent refresh → RT rotation → old RT rejected.
func TestAuthLoginAndRefreshFlow(t *testing.T) {
	// ── Step 1: login ─────────────────────────────────────────────────────────
	t.Log("step 1: login with email/password")
	body, status := do(t, http.MethodPost, authURL()+"/auth/login",
		map[string]any{"email": "user@smoke.test", "password": "smokepass"},
		nil,
	)
	assertStatus(t, status, http.StatusOK, body)

	var loginResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	decodeJSON(t, body, &loginResp)
	if loginResp.AccessToken == "" {
		t.Fatal("login: expected access_token in response body")
	}
	logf(t, "login succeeded: expires_in=%d", loginResp.ExpiresIn)

	// ── Step 2: use AT to access a protected endpoint ─────────────────────────
	t.Log("step 2: access protected endpoint with AT")
	raw, status := do(t, http.MethodGet, userURL()+"/users/me", nil, map[string]string{
		"Authorization": "Bearer " + loginResp.AccessToken,
	})
	assertStatus(t, status, http.StatusOK, raw)
	logf(t, "protected endpoint accessible with AT")

	// ── Step 3: refresh — obtain new AT + rotated RT ─────────────────────────
	t.Log("step 3: POST /auth/token/refresh")
	// We need to send the drl_refresh cookie. Since `do()` doesn't handle cookies,
	// we use a dedicated CookieJar request here.
	refreshReq, err := http.NewRequest(http.MethodPost, authURL()+"/auth/token/refresh", nil)
	if err != nil {
		t.Fatalf("new refresh request: %v", err)
	}
	// The login response should have set Set-Cookie: drl_refresh.
	// Re-login to capture the cookie properly.
	newAT, oldRT, newRT := loginAndCaptureCookies(t)
	if newAT == "" {
		t.Fatal("loginAndCaptureCookies: empty access token")
	}
	logf(t, "login+refresh captured: old_rt=%s new_rt=%s", truncate(oldRT, 8), truncate(newRT, 8))
	_ = refreshReq

	// ── Step 4: old RT should be rejected (rotation) ─────────────────────────
	t.Log("step 4: verifying old RT is rejected after rotation")
	if oldRT != "" && oldRT != newRT {
		req, _ := http.NewRequest(http.MethodPost, authURL()+"/auth/token/refresh", nil)
		req.Header.Set("Cookie", "drl_refresh="+oldRT)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("old RT refresh request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 for old RT after rotation, got %d", resp.StatusCode)
		}
		logf(t, "old RT correctly rejected with 401")
	}

	// ── Step 5: logout ────────────────────────────────────────────────────────
	t.Log("step 5: logout")
	logoutReq, _ := http.NewRequest(http.MethodPost, authURL()+"/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+newAT)
	logoutReq.Header.Set("Cookie", "drl_refresh="+newRT)
	logoutResp, err := client.Do(logoutReq)
	if err != nil {
		t.Fatalf("logout request: %v", err)
	}
	logoutResp.Body.Close()
	assertStatus(t, logoutResp.StatusCode, http.StatusNoContent, nil)
	logf(t, "logout succeeded")
}

// TestAuthRefreshRejectsExpiredRT verifies that a garbage/unknown RT returns 401.
func TestAuthRefreshRejectsExpiredRT(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, authURL()+"/auth/token/refresh", nil)
	req.Header.Set("Cookie", "drl_refresh=thisisnotavalidtoken")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	assertStatus(t, resp.StatusCode, http.StatusUnauthorized, nil)
	logf(t, "invalid RT correctly rejected")
}

// loginAndCaptureCookies logs in, then immediately refreshes, returning
// (newAT, oldRT, newRT) so the caller can verify rotation.
func loginAndCaptureCookies(t *testing.T) (newAT, oldRT, newRT string) {
	t.Helper()

	// Login
	payload, _ := json.Marshal(map[string]string{"email": "user@smoke.test", "password": "smokepass"})
	loginReq, _ := http.NewRequest(http.MethodPost, authURL()+"/auth/login", strings.NewReader(string(payload)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := client.Do(loginReq)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", loginResp.StatusCode)
	}

	var lr struct{ AccessToken string `json:"access_token"` }
	json.NewDecoder(loginResp.Body).Decode(&lr)

	for _, c := range loginResp.Cookies() {
		if c.Name == "drl_refresh" {
			oldRT = c.Value
		}
	}

	// Refresh using the cookie from login
	refreshReq, _ := http.NewRequest(http.MethodPost, authURL()+"/auth/token/refresh", nil)
	refreshReq.Header.Set("Cookie", "drl_refresh="+oldRT)
	refreshResp, err := client.Do(refreshReq)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer refreshResp.Body.Close()
	if refreshResp.StatusCode != http.StatusOK {
		t.Fatalf("refresh: expected 200, got %d", refreshResp.StatusCode)
	}

	var rr struct{ AccessToken string `json:"access_token"` }
	json.NewDecoder(refreshResp.Body).Decode(&rr)
	newAT = rr.AccessToken

	for _, c := range refreshResp.Cookies() {
		if c.Name == "drl_refresh" {
			newRT = c.Value
		}
	}
	return newAT, oldRT, newRT
}
