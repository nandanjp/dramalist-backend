package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"dramalist/user-service/db"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ── Mock store ────────────────────────────────────────────────────────────────

type mockStore struct {
	profile    *db.ProfileRow
	prefs      *db.PreferencesRow
	stats      *db.StatsRow
	upsertErr  error
	profileErr error
	updateErr  error
}

func (m *mockStore) UpsertProfile(_ context.Context, _, _, _ string) error { return m.upsertErr }
func (m *mockStore) UpsertPreferences(_ context.Context, _ string) error   { return m.upsertErr }

func (m *mockStore) GetProfile(_ context.Context, _ string) (db.ProfileRow, error) {
	if m.profileErr != nil {
		return db.ProfileRow{}, m.profileErr
	}
	if m.profile == nil {
		return db.ProfileRow{}, pgx.ErrNoRows
	}
	return *m.profile, nil
}

func (m *mockStore) GetPreferences(_ context.Context, _ string) (*db.PreferencesRow, error) {
	return m.prefs, nil
}

func (m *mockStore) GetStats(_ context.Context, _ string) (*db.StatsRow, error) {
	return m.stats, nil
}

func (m *mockStore) UpdateProfile(_ context.Context, _ string, _ db.ProfilePatch) error {
	return m.updateErr
}

func (m *mockStore) UpdatePreferences(_ context.Context, _ string, _ db.PrefsPatch) error {
	return m.updateErr
}

func (m *mockStore) GetProfileBySlug(_ context.Context, _ string) (db.ProfileRow, error) {
	if m.profileErr != nil {
		return db.ProfileRow{}, m.profileErr
	}
	if m.profile == nil {
		return db.ProfileRow{}, pgx.ErrNoRows
	}
	return *m.profile, nil
}

func (m *mockStore) AdminListUsers(_ context.Context, _ string, _, _ int) ([]db.ProfileRow, int64, error) {
	if m.profile != nil {
		return []db.ProfileRow{*m.profile}, 1, nil
	}
	return []db.ProfileRow{}, 0, nil
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func newTestHandler(store db.Store) *Handler {
	return New(nil, store, nil)
}

func testRouter(h *Handler) *gin.Engine {
	r := gin.New()
	h.Register(r)
	return r
}

func doRequest(r *gin.Engine, method, path string, body string, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return m
}

var bypassHeaders = map[string]string{
	"X-User-Id":    "00000000-0000-0000-0000-000000000001",
	"X-User-Email": "dev@dramalist.local",
}

var adminHeaders = map[string]string{
	"X-User-Id":    "00000000-0000-0000-0000-000000000001",
	"X-User-Email": "dev@dramalist.local",
	"X-User-Role":  "admin",
}

var testProfile = &db.ProfileRow{
	ID:          "00000000-0000-0000-0000-000000000001",
	Email:       "dev@dramalist.local",
	DisplayName: "Dev Admin",
	IsPublic:    true,
	CreatedAt:   "2024-01-01T00:00:00Z",
	UpdatedAt:   "2024-01-01T00:00:00Z",
}

// ── GET /users/me tests ───────────────────────────────────────────────────────

func TestGetMe_MissingUserID(t *testing.T) {
	r := testRouter(newTestHandler(&mockStore{}))
	w := doRequest(r, http.MethodGet, "/users/me", "", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetMe_UpsertError(t *testing.T) {
	store := &mockStore{upsertErr: errors.New("db error")}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/me", "", bypassHeaders)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestGetMe_Success(t *testing.T) {
	store := &mockStore{profile: testProfile}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/me", "", bypassHeaders)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w)
	profile, ok := body["profile"].(map[string]any)
	if !ok {
		t.Fatal("response missing 'profile' object")
	}
	if profile["id"] != testProfile.ID {
		t.Errorf("profile id: got %v, want %s", profile["id"], testProfile.ID)
	}
	if profile["email"] != testProfile.Email {
		t.Errorf("profile email: got %v, want %s", profile["email"], testProfile.Email)
	}
}

// ── PATCH /users/me tests ─────────────────────────────────────────────────────

func TestPatchMe_MissingUserID(t *testing.T) {
	r := testRouter(newTestHandler(&mockStore{}))
	w := doRequest(r, http.MethodPatch, "/users/me", `{"bio":"test"}`, nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestPatchMe_InvalidSlug(t *testing.T) {
	store := &mockStore{profile: testProfile}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodPatch, "/users/me", `{"profile_slug":"X"}`, bypassHeaders)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestPatchMe_SlugConflict(t *testing.T) {
	store := &mockStore{
		profile:   testProfile,
		updateErr: errors.New(`ERROR: duplicate key value violates unique constraint "profiles_profile_slug_key"`),
	}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodPatch, "/users/me", `{"profile_slug":"dev-admin"}`, bypassHeaders)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestPatchMe_Success(t *testing.T) {
	store := &mockStore{profile: testProfile}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodPatch, "/users/me", `{"bio":"updated bio"}`, bypassHeaders)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ── GET /users/me/stats tests ─────────────────────────────────────────────────

func TestGetMyStats_MissingUserID(t *testing.T) {
	r := testRouter(newTestHandler(&mockStore{}))
	w := doRequest(r, http.MethodGet, "/users/me/stats", "", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetMyStats_NoStats(t *testing.T) {
	store := &mockStore{} // stats == nil → returns empty stats
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/me/stats", "", bypassHeaders)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["genre_breakdown"] == nil {
		t.Error("expected genre_breakdown in response")
	}
}

func TestGetMyStats_WithData(t *testing.T) {
	rating := 9.5
	store := &mockStore{
		stats: &db.StatsRow{
			TotalWatched:   5,
			TotalEpisodes:  98,
			AvgRating:      &rating,
			GenreBreakdown: map[string]int{"romance": 3},
		},
	}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/me/stats", "", bypassHeaders)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if total, ok := body["total_watched"].(float64); !ok || int(total) != 5 {
		t.Errorf("expected total_watched=5, got %v", body["total_watched"])
	}
}

// ── GET /users/:slug tests ────────────────────────────────────────────────────

func TestGetBySlug_NotFound(t *testing.T) {
	store := &mockStore{} // profile == nil → pgx.ErrNoRows
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/some-slug", "", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetBySlug_PrivateProfile(t *testing.T) {
	private := *testProfile
	private.IsPublic = false
	store := &mockStore{profile: &private}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/dev-admin", "", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for private profile, got %d", w.Code)
	}
}

func TestGetBySlug_Success(t *testing.T) {
	slug := "dev-admin"
	public := *testProfile
	public.IsPublic = true
	public.ProfileSlug = &slug
	store := &mockStore{profile: &public}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/dev-admin", "", nil)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["id"] != testProfile.ID {
		t.Errorf("profile id: got %v, want %s", body["id"], testProfile.ID)
	}
}

// ── GET /users/admin/list tests ───────────────────────────────────────────────

func TestAdminListUsers_Forbidden(t *testing.T) {
	r := testRouter(newTestHandler(&mockStore{}))
	w := doRequest(r, http.MethodGet, "/users/admin/list", "", bypassHeaders)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestAdminListUsers_ForbiddenNoRole(t *testing.T) {
	r := testRouter(newTestHandler(&mockStore{}))
	w := doRequest(r, http.MethodGet, "/users/admin/list", "", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestAdminListUsers_Success(t *testing.T) {
	store := &mockStore{profile: testProfile}
	r := testRouter(newTestHandler(store))
	w := doRequest(r, http.MethodGet, "/users/admin/list", "", adminHeaders)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["users"] == nil {
		t.Error("expected users array in response")
	}
	if body["total"] == nil {
		t.Error("expected total in response")
	}
}
