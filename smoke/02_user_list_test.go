package smoke_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestUserListFlow exercises the full user list mutation chain:
//   show-service (write) → Kafka user.events (published, no consumer yet)
//   → show-service Redis cache invalidated
//   user-service GET /stats → show-service internal aggregate
func TestUserListFlow(t *testing.T) {
	catalogID := requireCatalogEntry(t)

	// ── Step 1: add entry to list ─────────────────────────────────────────────
	t.Log("step 1: adding catalog entry to user list")
	body, status := do(t, http.MethodPost, showURL()+"/list",
		map[string]any{
			"catalog_id":       catalogID,
			"status":           "watching",
			"episodes_watched": 3,
			"is_public":        true,
		},
		userHeaders(),
	)
	assertStatus(t, status, http.StatusCreated, body)
	entryID := jsonField(t, body, "id")
	logf(t, "list entry created: entry_id=%s", entryID)

	// ── Step 2: verify the entry appears in the user's list ──────────────────
	t.Log("step 2: reading user list")
	raw, status := do(t, http.MethodGet, showURL()+"/list", nil, userHeaders())
	assertStatus(t, status, http.StatusOK, raw)
	var listResp struct {
		Items []struct {
			ID        string `json:"id"`
			CatalogID string `json:"catalog_id"`
			Status    string `json:"status"`
		} `json:"items"`
	}
	decodeJSON(t, raw, &listResp)
	found := false
	for _, item := range listResp.Items {
		if item.ID == entryID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("entry %s not found in user list", entryID)
	}

	// ── Step 3: watch stats reflect the entry ────────────────────────────────
	t.Log("step 3: fetching watch stats via user-service")
	pollUntil(t, "watch stats > 0", 10*time.Second, time.Second, func() (bool, error) {
		raw, s := do(t, http.MethodGet,
			fmt.Sprintf("%s/users/%s/stats", userURL(), testUserID),
			nil, userHeaders(),
		)
		if s != http.StatusOK {
			return false, fmt.Errorf("user-service stats returned %d", s)
		}
		total := jsonFieldFloat(t, raw, "total")
		return total > 0, nil
	})
	logf(t, "watch stats confirmed non-zero")

	// ── Step 4: update the entry ─────────────────────────────────────────────
	t.Log("step 4: updating list entry to completed")
	raw, status = do(t, http.MethodPatch,
		fmt.Sprintf("%s/list/%s", showURL(), entryID),
		map[string]any{"status": "completed", "episodes_watched": 16},
		userHeaders(),
	)
	assertStatus(t, status, http.StatusOK, raw)
	updatedStatus := jsonField(t, raw, "status")
	if updatedStatus != "completed" {
		t.Fatalf("expected status=completed, got %s", updatedStatus)
	}

	// ── Step 5: delete the entry ──────────────────────────────────────────────
	t.Log("step 5: deleting list entry")
	raw, status = do(t, http.MethodDelete,
		fmt.Sprintf("%s/list/%s", showURL(), entryID),
		nil, userHeaders(),
	)
	assertStatus(t, status, http.StatusNoContent, raw)
	logf(t, "list entry deleted successfully")
}

// requireCatalogEntry returns a catalog_id, importing the stub TMDB entry if needed.
// Used by flows that depend on a catalog entry existing.
func requireCatalogEntry(t *testing.T) string {
	t.Helper()

	// Try to find an existing entry by TMDB ID first.
	raw, status := do(t, http.MethodGet,
		fmt.Sprintf("%s/catalog?tmdb_id=%d", showURL(), stubTMDBID),
		nil, adminHeaders(),
	)
	if status == http.StatusOK {
		var resp struct {
			Items []struct{ ID string `json:"id"` } `json:"items"`
		}
		decodeJSON(t, raw, &resp)
		if len(resp.Items) > 0 {
			return resp.Items[0].ID
		}
	}

	// Not found — import it now.
	body, s := do(t, http.MethodPost, dramaURL()+"/drama/import",
		map[string]any{"tmdb_id": stubTMDBID},
		adminHeaders(),
	)
	if s != http.StatusCreated {
		t.Fatalf("requireCatalogEntry: import returned %d\n%s", s, body)
	}
	id := jsonField(t, body, "id")
	if id == "" {
		t.Fatal("requireCatalogEntry: import returned empty id")
	}

	// Wait for the entry to be visible.
	pollUntil(t, "catalog entry visible in show-service", 10*time.Second, time.Second, func() (bool, error) {
		_, s := do(t, http.MethodGet, fmt.Sprintf("%s/catalog/%s", showURL(), id), nil, nil)
		return s == http.StatusOK, nil
	})

	return id
}
