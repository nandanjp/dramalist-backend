package db

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ActorParams struct {
	Name            string
	NativeName      *string
	Birthdate       *string
	Nationality     *string
	Biography       *string
	ProfileImageURL *string
	TMDBPersonID     int
}

func UpsertActor(ctx context.Context, pool *pgxpool.Pool, p ActorParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO actors (name, native_name, birthdate, nationality, biography, profile_image_url, tmdb_person_id)
		VALUES ($1, $2, $3::date, $4, $5, $6, $7)
		ON CONFLICT (tmdb_person_id) DO UPDATE
		  SET name              = EXCLUDED.name,
		      native_name       = COALESCE(EXCLUDED.native_name, actors.native_name),
		      birthdate         = COALESCE(EXCLUDED.birthdate, actors.birthdate),
		      nationality       = COALESCE(EXCLUDED.nationality, actors.nationality),
		      biography         = COALESCE(EXCLUDED.biography, actors.biography),
		      profile_image_url = COALESCE(EXCLUDED.profile_image_url, actors.profile_image_url)
		RETURNING id::text`,
		p.Name, p.NativeName, p.Birthdate, p.Nationality, p.Biography, p.ProfileImageURL, p.TMDBPersonID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}

	// If tmdb_person_id uniqueness conflicts with a manually-added actor row
	// that has no tmdb_person_id, fall back to a name lookup.
	if isUniqueViolation(err) || isNameConflict(err) {
		err2 := pool.QueryRow(ctx,
			`SELECT id::text FROM actors WHERE lower(name) = lower($1) LIMIT 1`, p.Name,
		).Scan(&id)
		if err2 == nil {
			return id, nil
		}
		if !errors.Is(err2, pgx.ErrNoRows) {
			return "", err2
		}
	}
	return "", err
}

func UpdateActorProfileImage(ctx context.Context, pool *pgxpool.Pool, actorID, url string) error {
	_, err := pool.Exec(ctx,
		`UPDATE actors SET profile_image_url = $1, updated_at = NOW() WHERE id = $2`,
		url, actorID,
	)
	return err
}

func InsertCastMember(ctx context.Context, pool *pgxpool.Pool,
	catalogID, actorID, characterName, role string, sortOrder int,
) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO cast_members (catalog_id, actor_id, character_name, role, sort_order)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (catalog_id, actor_id) DO NOTHING`,
		catalogID, actorID, characterName, role, sortOrder,
	)
	return err
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "unique")
}

func isNameConflict(err error) bool {
	return strings.Contains(err.Error(), "idx_actors_name_lower")
}
