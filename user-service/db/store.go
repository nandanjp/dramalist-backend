package db

import "context"

// ── Domain row types ──────────────────────────────────────────────────────────

type ProfileRow struct {
	ID          string
	Email       string
	DisplayName string
	AvatarURL   *string
	Bio         *string
	IsPublic    bool
	ProfileSlug *string
	CreatedAt   string
	UpdatedAt   string
}

type PreferencesRow struct {
	DefaultSort         string
	DefaultStatusFilter []string
	DefaultGenreFilter  []string
	UITheme             string
}

type StatsRow struct {
	TotalWatched   int
	TotalEpisodes  int
	AvgRating      *float64
	GenreBreakdown map[string]int
}

// ── Patch input types ─────────────────────────────────────────────────────────

type ProfilePatch struct {
	DisplayName *string
	AvatarURL   *string
	Bio         *string
	IsPublic    *bool
	ProfileSlug *string
}

type PrefsPatch struct {
	DefaultSort         *string
	DefaultStatusFilter *[]string
	DefaultGenreFilter  *[]string
	UITheme             *string
}

// ── Store interface ───────────────────────────────────────────────────────────

type Store interface {
	// UpsertProfile inserts a profile row if one does not already exist.
	UpsertProfile(ctx context.Context, id, email, displayName string) error
	// UpsertPreferences inserts a preferences row if one does not already exist.
	UpsertPreferences(ctx context.Context, userID string) error
	// GetProfile returns the profile row for the given user ID.
	GetProfile(ctx context.Context, userID string) (ProfileRow, error)
	// GetPreferences returns the preferences row for the given user ID.
	GetPreferences(ctx context.Context, userID string) (*PreferencesRow, error)
	// GetStats returns the watch_stats row for the given user ID.
	GetStats(ctx context.Context, userID string) (*StatsRow, error)
	// UpdateProfile applies non-nil fields from the patch to the profile row.
	// Returns a unique-constraint error (containing "unique") when profile_slug is taken.
	UpdateProfile(ctx context.Context, userID string, p ProfilePatch) error
	// UpdatePreferences applies non-nil fields from the patch to the preferences row.
	UpdatePreferences(ctx context.Context, userID string, p PrefsPatch) error
	// GetProfileBySlug returns the profile row for the given slug.
	GetProfileBySlug(ctx context.Context, slug string) (ProfileRow, error)
	// AdminListUsers returns paginated profiles filtered by optional search term q.
	// q is matched case-insensitively against email and display_name; empty q returns all.
	AdminListUsers(ctx context.Context, q string, page, limit int) ([]ProfileRow, int64, error)
}
