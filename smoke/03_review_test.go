package smoke_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestReviewFlow exercises the full review event chain:
//   review-service (write + in-process aggregate) → Kafka review.events
//   → search-service (Meilisearch rating patch)
func TestReviewFlow(t *testing.T) {
	catalogID := requireCatalogEntry(t)

	// ── Step 1: create a review ───────────────────────────────────────────────
	t.Log("step 1: creating review")
	body, status := do(t, http.MethodPost, reviewURL()+"/reviews",
		map[string]any{
			"catalog_id": catalogID,
			"rating":     8,
			"body":       "Smoke test review — solid performances and tight pacing.",
		},
		userHeaders(),
	)
	// 409 is acceptable if this test ran before (one review per user per catalog).
	if status != http.StatusCreated && status != http.StatusConflict {
		t.Fatalf("create review: expected 201 or 409, got %d\nbody: %s", status, body)
	}
	logf(t, "review created (or already existed): status=%d", status)

	// ── Step 2: verify aggregate updated in-process ───────────────────────────
	t.Log("step 2: verifying review aggregate")
	pollUntil(t, "review aggregate visible", 5*time.Second, 500*time.Millisecond, func() (bool, error) {
		raw, s := do(t, http.MethodGet,
			fmt.Sprintf("%s/reviews/aggregate/%s", reviewURL(), catalogID),
			nil, nil,
		)
		if s != http.StatusOK {
			return false, fmt.Errorf("aggregate endpoint returned %d", s)
		}
		count := jsonFieldFloat(t, raw, "review_count")
		return count > 0, nil
	})
	logf(t, "review aggregate confirmed")

	// ── Step 3: verify Meilisearch document updated with rating ───────────────
	t.Log("step 3: polling Meilisearch for updated avg_rating")
	pollUntil(t, "Meilisearch avg_rating > 0", 20*time.Second, 2*time.Second, func() (bool, error) {
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
		if len(resp.Hits) == 0 {
			return false, nil
		}
		avgRating, _ := resp.Hits[0]["avg_rating"].(float64)
		return avgRating > 0, nil
	})
	logf(t, "Meilisearch avg_rating updated via review.events")
}

// TestReviewUpdateAndDeleteFlow verifies aggregate recalculation on update/delete.
func TestReviewUpdateAndDeleteFlow(t *testing.T) {
	catalogID := requireCatalogEntry(t)

	// Ensure there is a review to work with.
	body, status := do(t, http.MethodPost, reviewURL()+"/reviews",
		map[string]any{"catalog_id": catalogID, "rating": 6, "body": "Update/delete smoke test."},
		userHeaders(),
	)
	var reviewID string
	if status == http.StatusCreated {
		reviewID = jsonField(t, body, "id")
	} else if status == http.StatusConflict {
		// Fetch existing review id.
		raw, s := do(t, http.MethodGet,
			fmt.Sprintf("%s/reviews?catalog_id=%s&user_id=%s", reviewURL(), catalogID, testUserID),
			nil, userHeaders(),
		)
		if s != http.StatusOK {
			t.Skipf("could not fetch existing review: status %d", s)
		}
		var resp struct {
			Items []struct{ ID string `json:"id"` } `json:"items"`
		}
		decodeJSON(t, raw, &resp)
		if len(resp.Items) == 0 {
			t.Skip("no existing review found to update")
		}
		reviewID = resp.Items[0].ID
	} else {
		t.Fatalf("unexpected status %d", status)
	}

	// Update the review.
	raw, status := do(t, http.MethodPatch,
		fmt.Sprintf("%s/reviews/%s", reviewURL(), reviewID),
		map[string]any{"rating": 9, "body": "Updated smoke test review."},
		userHeaders(),
	)
	assertStatus(t, status, http.StatusOK, raw)
	updatedRating := jsonFieldFloat(t, raw, "rating")
	if updatedRating != 9 {
		t.Fatalf("expected rating=9, got %v", updatedRating)
	}
	logf(t, "review updated to rating=9")

	// Delete the review.
	raw, status = do(t, http.MethodDelete,
		fmt.Sprintf("%s/reviews/%s", reviewURL(), reviewID),
		nil, userHeaders(),
	)
	assertStatus(t, status, http.StatusNoContent, raw)
	logf(t, "review deleted")
}
