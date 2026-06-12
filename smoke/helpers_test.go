// Package smoke contains end-to-end smoke tests that run against the full
// docker-compose.smoke.yml stack. Each test exercises one complete event flow
// from the system spec (services/system-spec/).
//
// Prerequisites:
//   make smoke-up      # start the stack
//   make smoke-test    # run tests
//   make smoke-down    # tear down
//
// Service URLs are read from env vars; defaults target localhost ports as
// exposed by docker-compose.smoke.yml.
package smoke_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// ── Well-known fixture values ─────────────────────────────────────────────────

const (
	adminUserID = "00000000-0000-0000-0000-000000000001"
	testUserID  = "00000000-0000-0000-0000-000000000002"

	// TMDB and AniList IDs that stub-server serves deterministic fixtures for.
	stubTMDBID    = 98765
	stubAniListID = 11111
)

// ── Service URLs ─────────────────────────────────────────────────────────────

func authURL() string   { return env("AUTH_SERVICE_URL", "http://localhost:3001") }
func userURL() string   { return env("USER_SERVICE_URL", "http://localhost:3002") }
func showURL() string   { return env("SHOW_SERVICE_URL", "http://localhost:3003") }
func reviewURL() string { return env("REVIEW_SERVICE_URL", "http://localhost:3004") }
func searchURL() string { return env("SEARCH_SERVICE_URL", "http://localhost:3005") }
func mediaURL() string  { return env("MEDIA_SERVICE_URL", "http://localhost:3006") }
func aiURL() string     { return env("AI_SERVICE_URL", "http://localhost:3007") }
func animeURL() string  { return env("ANIME_SERVICE_URL", "http://localhost:3008") }
func dramaURL() string  { return env("DRAMA_SERVICE_URL", "http://localhost:3009") }

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ── HTTP helpers ─────────────────────────────────────────────────────────────

var client = &http.Client{Timeout: 15 * time.Second}

// do performs an HTTP request and returns the response body bytes and status code.
func do(t *testing.T, method, url string, body any, headers map[string]string) ([]byte, int) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode
}

// adminHeaders returns headers that simulate the gateway injecting admin identity.
func adminHeaders() map[string]string {
	return map[string]string{
		"X-User-Id":   adminUserID,
		"X-User-Role": "admin",
	}
}

// userHeaders returns headers that simulate the gateway injecting a regular user.
func userHeaders() map[string]string {
	return map[string]string{
		"X-User-Id":   testUserID,
		"X-User-Role": "user",
	}
}

// decodeJSON unmarshals raw bytes into v, failing the test on error.
func decodeJSON(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, raw)
	}
}

// ── Polling helper ───────────────────────────────────────────────────────────

// pollUntil retries check every interval until it returns true or timeout elapses.
// Use this to wait for async Kafka propagation effects.
func pollUntil(t *testing.T, desc string, timeout, interval time.Duration, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		ok, err := check()
		if err != nil {
			lastErr = err
		}
		if ok {
			return
		}
		time.Sleep(interval)
	}
	if lastErr != nil {
		t.Fatalf("pollUntil %q: timed out after %s (last error: %v)", desc, timeout, lastErr)
	}
	t.Fatalf("pollUntil %q: condition not met within %s", desc, timeout)
}

// assertStatus fails the test if the status code does not match expected.
func assertStatus(t *testing.T, got, want int, body []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("expected status %d, got %d\nbody: %s", want, got, body)
	}
}

// jsonField extracts a top-level string field from a JSON object.
func jsonField(t *testing.T, raw []byte, field string) string {
	t.Helper()
	var m map[string]any
	decodeJSON(t, raw, &m)
	v, _ := m[field].(string)
	return v
}

// jsonFieldFloat extracts a top-level float64 field from a JSON object.
func jsonFieldFloat(t *testing.T, raw []byte, field string) float64 {
	t.Helper()
	var m map[string]any
	decodeJSON(t, raw, &m)
	v, _ := m[field].(float64)
	return v
}

// logf logs a formatted message prefixed with the test name.
func logf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Logf("[%s] %s", t.Name(), fmt.Sprintf(format, args...))
}
