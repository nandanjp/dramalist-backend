package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const catalogSelectCols = `id::text, media_type, title, original_title, synopsis, poster_url,
    year, country, language, episode_count, duration_minutes, genre,
    airing_status, tmdb_id, created_by::text, created_at, updated_at`

// PostgresQuerier implements Querier using a pgxpool.Pool.
type PostgresQuerier struct {
	pool *pgxpool.Pool
}

func NewPostgresQuerier(pool *pgxpool.Pool) *PostgresQuerier {
	return &PostgresQuerier{pool: pool}
}

func (q *PostgresQuerier) DiscoverCatalog(ctx context.Context, orderBy string, limit int) ([]CatalogRow, error) {
	rows, err := q.pool.Query(ctx,
		"SELECT "+catalogSelectCols+" FROM catalog ORDER BY "+orderBy+" LIMIT $1",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]CatalogRow, 0, limit)
	for rows.Next() {
		var c CatalogRow
		if err := rows.Scan(
			&c.ID, &c.MediaType, &c.Title, &c.OriginalTitle, &c.Synopsis, &c.PosterURL,
			&c.Year, &c.Country, &c.Language, &c.EpisodeCount, &c.DurationMinutes, &c.Genre,
			&c.AiringStatus, &c.TMDBID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if c.Genre == nil {
			c.Genre = []string{}
		}
		entries = append(entries, c)
	}
	return entries, rows.Err()
}

func (q *PostgresQuerier) GetCatalogDetail(ctx context.Context, id string) (*CatalogDetailRow, error) {
	var detail CatalogDetailRow
	err := q.pool.QueryRow(ctx,
		"SELECT "+catalogSelectCols+" FROM catalog WHERE id = $1", id,
	).Scan(
		&detail.ID, &detail.MediaType, &detail.Title, &detail.OriginalTitle, &detail.Synopsis, &detail.PosterURL,
		&detail.Year, &detail.Country, &detail.Language, &detail.EpisodeCount, &detail.DurationMinutes, &detail.Genre,
		&detail.AiringStatus, &detail.TMDBID, &detail.CreatedBy, &detail.CreatedAt, &detail.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if detail.Genre == nil {
		detail.Genre = []string{}
	}

	rows, err := q.pool.Query(ctx,
		`SELECT cm.id::text, cm.actor_id::text, a.name, a.profile_image_url, cm.character_name, cm.role, cm.sort_order
		 FROM cast_members cm
		 JOIN actors a ON a.id = cm.actor_id
		 WHERE cm.catalog_id = $1
		 ORDER BY cm.sort_order, a.name`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	detail.Cast = make([]CastMemberRow, 0)
	for rows.Next() {
		var m CastMemberRow
		if err := rows.Scan(&m.CastID, &m.ActorID, &m.ActorName, &m.ProfileImageURL, &m.CharacterName, &m.Role, &m.SortOrder); err != nil {
			return nil, err
		}
		detail.Cast = append(detail.Cast, m)
	}
	return &detail, rows.Err()
}

func (q *PostgresQuerier) GetActorDetail(ctx context.Context, id string) (*ActorDetailRow, error) {
	var detail ActorDetailRow
	err := q.pool.QueryRow(ctx,
		`SELECT id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, tmdb_person_id, created_at, updated_at
		 FROM actors WHERE id = $1`, id,
	).Scan(&detail.ID, &detail.Name, &detail.NativeName, &detail.Birthdate, &detail.Nationality,
		&detail.Biography, &detail.ProfileImageURL, &detail.TMDBPersonID, &detail.CreatedAt, &detail.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := q.pool.Query(ctx,
		`SELECT cm.id::text, c.id::text, c.media_type, c.title, c.original_title, c.poster_url, c.year,
		        cm.character_name, cm.role, cm.sort_order
		 FROM cast_members cm
		 JOIN catalog c ON c.id = cm.catalog_id
		 WHERE cm.actor_id = $1
		 ORDER BY c.year DESC NULLS LAST, c.title ASC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	detail.Filmography = make([]FilmographyRow, 0)
	for rows.Next() {
		var f FilmographyRow
		if err := rows.Scan(&f.CastID, &f.CatalogID, &f.MediaType, &f.Title, &f.OriginalTitle, &f.PosterURL, &f.Year,
			&f.CharacterName, &f.Role, &f.SortOrder); err != nil {
			return nil, err
		}
		detail.Filmography = append(detail.Filmography, f)
	}
	return &detail, rows.Err()
}
