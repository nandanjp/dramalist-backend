package smoke_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAIRecommendationsFlow verifies the on-demand AI recommendation path:
//   ai-service assembles taste profile → calls Ollama stub → streams SSE → caches result.
func TestAIRecommendationsFlow(t *testing.T) {
	// Ensure the user has at least one list entry so the taste profile is non-empty.
	catalogID := requireCatalogEntry(t)
	body, s := do(t, http.MethodPost, showURL()+"/list",
		map[string]any{"catalog_id": catalogID, "status": "completed", "episodes_watched": 16},
		userHeaders(),
	)
	if s != http.StatusCreated && s != http.StatusConflict {
		t.Logf("list entry pre-condition: status=%d body=%s", s, body)
	}

	// ── Step 1: request list_based recommendation ─────────────────────────────
	t.Log("step 1: requesting list_based recommendation (SSE)")
	fullText := sseRequest(t, aiURL()+"/ai/recommend",
		map[string]any{"mode": "list_based"},
		userHeaders(),
		30*time.Second,
	)
	if fullText == "" {
		t.Fatal("expected non-empty recommendation text from SSE stream")
	}
	logf(t, "recommendation received: %q", truncate(fullText, 80))

	// ── Step 2: second call should hit result cache (fast path) ───────────────
	t.Log("step 2: second call should be served from cache")
	start := time.Now()
	cachedText := sseRequest(t, aiURL()+"/ai/recommend",
		map[string]any{"mode": "list_based"},
		userHeaders(),
		10*time.Second,
	)
	elapsed := time.Since(start)
	if cachedText == "" {
		t.Fatal("cache hit: expected non-empty text")
	}
	logf(t, "cache hit in %s: %q", elapsed, truncate(cachedText, 80))
	if elapsed > 3*time.Second {
		t.Logf("warning: cache hit took %s — may indicate cache miss", elapsed)
	}
}

// TestAIShowAnalysisMode verifies the show_analysis mode.
func TestAIShowAnalysisMode(t *testing.T) {
	catalogID := requireCatalogEntry(t)
	text := sseRequest(t, aiURL()+"/ai/recommend",
		map[string]any{"mode": "show_analysis", "catalog_id": catalogID},
		userHeaders(), 30*time.Second,
	)
	if text == "" {
		t.Fatal("show_analysis: expected non-empty SSE stream")
	}
	logf(t, "show_analysis: %q", truncate(text, 80))
}

// TestAISimilarToMode verifies the similar_to mode.
func TestAISimilarToMode(t *testing.T) {
	catalogID := requireCatalogEntry(t)
	text := sseRequest(t, aiURL()+"/ai/recommend",
		map[string]any{"mode": "similar_to", "catalog_id": catalogID},
		userHeaders(), 30*time.Second,
	)
	if text == "" {
		t.Fatal("similar_to: expected non-empty SSE stream")
	}
	logf(t, "similar_to: %q", truncate(text, 80))
}

// sseRequest opens an SSE POST and collects all data lines until "data: [DONE]".
func sseRequest(t *testing.T, url string, body any, headers map[string]string, timeout time.Duration) string {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("sseRequest marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("sseRequest new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	streamClient := &http.Client{Timeout: timeout}
	resp, err := streamClient.Do(req)
	if err != nil {
		t.Fatalf("sseRequest do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sseRequest: expected 200, got %d", resp.StatusCode)
	}

	var sb strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		sb.WriteString(data)
	}
	return sb.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
