package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CatalogInsertParams struct {
	MediaType       string
	Title           string
	AiringStatus    string
	CreatedBy       string
	OriginalTitle   *string
	Synopsis        *string
	PosterURL       *string
	Country         *string
	Language        *string
	Year            *int
	EpisodeCount    *int
	DurationMinutes *int
	Genre           []string
	TMDBID          int
}

func CheckTMDBExists(ctx context.Context, pool *pgxpool.Pool, tmdbID int) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM catalog WHERE tmdb_id = $1)`, tmdbID,
	).Scan(&exists)
	return exists, err
}

func InsertCatalog(ctx context.Context, pool *pgxpool.Pool, p CatalogInsertParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO catalog
		  (media_type, title, original_title, synopsis, poster_url,
		   year, country, language, episode_count, duration_minutes,
		   genre, airing_status, tmdb_id, created_by)
		VALUES
		  ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id::text`,
		p.MediaType, p.Title, p.OriginalTitle, p.Synopsis, p.PosterURL,
		p.Year, p.Country, p.Language, p.EpisodeCount, p.DurationMinutes,
		p.Genre, p.AiringStatus, p.TMDBID, p.CreatedBy,
	).Scan(&id)
	return id, err
}
