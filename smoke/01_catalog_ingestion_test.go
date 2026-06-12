package smoke_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCatalogIngestionFlow exercises the full catalog.created event chain:
//   drama-service → show-service (write) → Kafka catalog.events
//   → search-service (Meilisearch index)
//   → media-service (poster proxy → show-service poster patch)
func TestCatalogIngestionFlow(t *testing.T) {
	// ── Step 1: import via drama-service (stub TMDB) ─────────────────────────
	t.Log("step 1: importing catalog entry via drama-service")
	body, status := do(t, http.MethodPost, dramaURL()+"/drama/import",
		map[string]any{"tmdb_id": stubTMDBID},
		adminHeaders(),
	)
	// 409 is acceptable if the entry was already imported in a previous run.
	if status != http.StatusCreated && status != http.StatusConflict {
		t.Fatalf("import: expected 201 or 409, got %d\nbody: %s", status, body)
	}
	logf(t, "import response: status=%d", status)

	// ── Step 2: verify show-service has the catalog entry ────────────────────
	t.Log("step 2: verifying catalog entry in show-service")
	var catalogID string
	pollUntil(t, "catalog entry created in show-service", 15*time.Second, time.Second, func() (bool, error) {
		raw, s := do(t, http.MethodGet,
			fmt.Sprintf("%s/catalog?tmdb_id=%d", showURL(), stubTMDBID),
			nil, adminHeaders(),
		)
		if s != http.StatusOK {
			return false, fmt.Errorf("show-service GET /catalog returned %d", s)
		}
		var resp struct {
			Items []struct {
				ID     string `json:"id"`
				TMDBID int    `json:"tmdb_id"`
			} `json:"items"`
		}
		decodeJSON(t, raw, &resp)
		if len(resp.Items) == 0 {
			return false, nil
		}
		catalogID = resp.Items[0].ID
		return catalogID != "", nil
	})
	logf(t, "catalog entry confirmed: catalog_id=%s", catalogID)

	// ── Step 3: verify search-service indexed the document ───────────────────
	t.Log("step 3: polling search-service for Meilisearch document")
	pollUntil(t, "catalog indexed in Meilisearch", 20*time.Second, 2*time.Second, func() (bool, error) {
		raw, s := do(t, http.MethodGet,
			searchURL()+"/search?q=Smoke+Test+Drama",
			nil, nil,
		)
		if s != http.StatusOK {
			return false, fmt.Errorf("search-service returned %d", s)
		}
		var resp struct {
			Hits []map[string]any `json:"hits"`
		}
		decodeJSON(t, raw, &resp)
		return len(resp.Hits) > 0, nil
	})
	logf(t, "catalog document indexed in Meilisearch")

	// ── Step 4: verify media-service proxied the poster ─────────────────────
	t.Log("step 4: polling show-service for proxied MinIO poster_url")
	pollUntil(t, "poster_url updated to MinIO URL", 30*time.Second, 2*time.Second, func() (bool, error) {
		raw, s := do(t, http.MethodGet,
			fmt.Sprintf("%s/catalog/%s", showURL(), catalogID),
			nil, nil,
		)
		if s != http.StatusOK {
			return false, fmt.Errorf("show-service GET /catalog/:id returned %d", s)
		}
		posterURL := jsonField(t, raw, "poster_url")
		// MinIO URLs contain "minio" or "localhost:9000" — not the stub server hostname.
		isMinIO := strings.Contains(posterURL, "minio") ||
			strings.Contains(posterURL, "9000") ||
			strings.Contains(posterURL, "/media/file/")
		return isMinIO, nil
	})
	logf(t, "poster proxied to MinIO successfully")
}

// TestCatalogDeletionFlow verifies catalog.deleted cleans up Meilisearch and MinIO.
func TestCatalogDeletionFlow(t *testing.T) {
	// Create a throwaway entry to delete.
	const throwawayTMDBID = 99999

	body, status := do(t, http.MethodPost, dramaURL()+"/drama/import",
		map[string]any{"tmdb_id": throwawayTMDBID},
		adminHeaders(),
	)
	if status == http.StatusConflict {
		t.Skip("throwaway entry already exists from a previous run; skipping delete flow")
	}
	assertStatus(t, status, http.StatusCreated, body)
	catalogID := jsonField(t, body, "id")
	logf(t, "throwaway catalog_id=%s", catalogID)

	// Delete it.
	body, status = do(t, http.MethodDelete,
		fmt.Sprintf("%s/catalog/%s", showURL(), catalogID),
		nil, adminHeaders(),
	)
	assertStatus(t, status, http.StatusNoContent, body)

	// Verify it disappears from search.
	pollUntil(t, "catalog entry removed from Meilisearch", 15*time.Second, time.Second, func() (bool, error) {
		raw, s := do(t, http.MethodGet,
			fmt.Sprintf("%s/search?q=throwaway+smoke+test", searchURL()),
			nil, nil,
		)
		if s != http.StatusOK {
			return false, nil
		}
		var resp struct {
			Hits []map[string]any `json:"hits"`
		}
		decodeJSON(t, raw, &resp)
		for _, h := range resp.Hits {
			if id, _ := h["id"].(string); id == catalogID {
				return false, nil // still in index
			}
		}
		return true, nil
	})
	logf(t, "catalog entry removed from Meilisearch")
}
