package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CatalogInsertParams struct {
	MediaType       string
	Title           string
	OriginalTitle   *string
	Synopsis        *string
	PosterURL       *string
	Year            *int
	Country         *string
	Language        *string
	EpisodeCount    *int
	DurationMinutes *int
	Genre           []string
	AiringStatus    string
	AnilistID       int
	Studio          *string
	AnimeFormat     string
	CreatedBy       string
}

func CheckAnilistExists(ctx context.Context, pool *pgxpool.Pool, anilistID int) (bool, error) {
	var id string
	err := pool.QueryRow(ctx,
		"SELECT id::text FROM catalog WHERE anilist_id = $1", anilistID,
	).Scan(&id)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func InsertCatalog(ctx context.Context, pool *pgxpool.Pool, p CatalogInsertParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO catalog (
			media_type, title, original_title, synopsis, poster_url,
			year, country, language, episode_count, duration_minutes,
			genre, airing_status, anilist_id, studio, anime_format, created_by
		) VALUES (
			$1,  $2,  $3,  $4,  $5,
			$6,  $7,  $8,  $9,  $10,
			$11, $12, $13, $14, $15, $16
		) RETURNING id::text`,
		p.MediaType, p.Title, p.OriginalTitle, p.Synopsis, p.PosterURL,
		p.Year, p.Country, p.Language, p.EpisodeCount, p.DurationMinutes,
		p.Genre, p.AiringStatus, p.AnilistID, p.Studio, p.AnimeFormat, p.CreatedBy,
	).Scan(&id)
	return id, err
}
