package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store using a pgxpool.Pool.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) UpsertProfile(ctx context.Context, id, email, displayName string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO profiles (id, email, display_name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (id) DO NOTHING`,
		id, email, displayName,
	)
	return err
}

func (s *PostgresStore) UpsertPreferences(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO preferences (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING",
		userID,
	)
	return err
}

func (s *PostgresStore) GetProfile(ctx context.Context, userID string) (ProfileRow, error) {
	var p ProfileRow
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, email, display_name, avatar_url, bio, is_public, profile_slug,
		        created_at::text, updated_at::text
		 FROM profiles WHERE id = $1`,
		userID,
	).Scan(&p.ID, &p.Email, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.IsPublic, &p.ProfileSlug,
		&p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *PostgresStore) GetPreferences(ctx context.Context, userID string) (*PreferencesRow, error) {
	var p PreferencesRow
	var statusFilter, genreFilter []string
	err := s.pool.QueryRow(ctx,
		`SELECT default_sort,
		        COALESCE(default_status_filter, '{}'),
		        COALESCE(default_genre_filter, '{}'),
		        ui_theme
		 FROM preferences WHERE user_id = $1`,
		userID,
	).Scan(&p.DefaultSort, &statusFilter, &genreFilter, &p.UITheme)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if statusFilter == nil {
		statusFilter = []string{}
	}
	if genreFilter == nil {
		genreFilter = []string{}
	}
	p.DefaultStatusFilter = statusFilter
	p.DefaultGenreFilter = genreFilter
	return &p, nil
}

func (s *PostgresStore) GetStats(ctx context.Context, userID string) (*StatsRow, error) {
	var stats StatsRow
	var genreBytes []byte
	err := s.pool.QueryRow(ctx,
		"SELECT total_watched, total_episodes, avg_rating, genre_breakdown FROM watch_stats WHERE user_id = $1",
		userID,
	).Scan(&stats.TotalWatched, &stats.TotalEpisodes, &stats.AvgRating, &genreBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stats.GenreBreakdown = make(map[string]int)
	json.Unmarshal(genreBytes, &stats.GenreBreakdown) //nolint:errcheck — safe default
	return &stats, nil
}

func (s *PostgresStore) UpdateProfile(ctx context.Context, userID string, p ProfilePatch) error {
	args := []any{userID}
	cols := []string{}
	n := 2

	if p.DisplayName != nil {
		cols = append(cols, fmt.Sprintf("display_name = $%d", n))
		args = append(args, *p.DisplayName)
		n++
	}
	if p.AvatarURL != nil {
		cols = append(cols, fmt.Sprintf("avatar_url = $%d", n))
		args = append(args, *p.AvatarURL)
		n++
	}
	if p.Bio != nil {
		cols = append(cols, fmt.Sprintf("bio = $%d", n))
		args = append(args, *p.Bio)
		n++
	}
	if p.IsPublic != nil {
		cols = append(cols, fmt.Sprintf("is_public = $%d", n))
		args = append(args, *p.IsPublic)
		n++
	}
	if p.ProfileSlug != nil {
		cols = append(cols, fmt.Sprintf("profile_slug = $%d", n))
		args = append(args, *p.ProfileSlug)
		n++
	}

	if len(cols) == 0 {
		return nil
	}
	cols = append(cols, "updated_at = NOW()")
	query := "UPDATE profiles SET " + strings.Join(cols, ", ") + " WHERE id = $1"
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *PostgresStore) UpdatePreferences(ctx context.Context, userID string, p PrefsPatch) error {
	args := []any{userID}
	cols := []string{}
	n := 2

	if p.DefaultSort != nil {
		cols = append(cols, fmt.Sprintf("default_sort = $%d", n))
		args = append(args, *p.DefaultSort)
		n++
	}
	if p.DefaultStatusFilter != nil {
		cols = append(cols, fmt.Sprintf("default_status_filter = $%d", n))
		args = append(args, *p.DefaultStatusFilter)
		n++
	}
	if p.DefaultGenreFilter != nil {
		cols = append(cols, fmt.Sprintf("default_genre_filter = $%d", n))
		args = append(args, *p.DefaultGenreFilter)
		n++
	}
	if p.UITheme != nil {
		cols = append(cols, fmt.Sprintf("ui_theme = $%d", n))
		args = append(args, *p.UITheme)
		n++
	}

	if len(cols) == 0 {
		return nil
	}
	cols = append(cols, "updated_at = NOW()")
	query := "UPDATE preferences SET " + strings.Join(cols, ", ") + " WHERE user_id = $1"
	_, err := s.pool.Exec(ctx, query, args...)
	return err
}

func (s *PostgresStore) GetProfileBySlug(ctx context.Context, slug string) (ProfileRow, error) {
	var p ProfileRow
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, email, display_name, avatar_url, bio, is_public, profile_slug
		 FROM profiles WHERE profile_slug = $1`,
		slug,
	).Scan(&p.ID, &p.Email, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.IsPublic, &p.ProfileSlug)
	return p, err
}

func (s *PostgresStore) AdminListUsers(ctx context.Context, q string, page, limit int) ([]ProfileRow, int64, error) {
	offset := (page - 1) * limit

	// search is the LIKE pattern; empty string signals "no filter".
	search := ""
	if q != "" {
		search = "%" + strings.ToLower(q) + "%"
	}

	const where = `($1 = '' OR LOWER(email) LIKE $1 OR LOWER(display_name) LIKE $1)`

	var total int64
	if err := s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM profiles WHERE "+where, search,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	pgRows, err := s.pool.Query(ctx,
		`SELECT id::text, email, display_name, avatar_url, bio, is_public, profile_slug,
		        created_at::text, updated_at::text
		 FROM profiles WHERE `+where+`
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		search, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer pgRows.Close()

	users := make([]ProfileRow, 0)
	for pgRows.Next() {
		var r ProfileRow
		if err := pgRows.Scan(&r.ID, &r.Email, &r.DisplayName, &r.AvatarURL, &r.Bio,
			&r.IsPublic, &r.ProfileSlug, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		users = append(users, r)
	}
	return users, total, nil
}
