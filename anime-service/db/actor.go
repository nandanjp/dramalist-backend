package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ActorParams struct {
	Name            string
	NativeName      *string
	Nationality     *string
	Birthdate       *time.Time
	ProfileImageURL *string
	Biography       *string
	AnilistPersonID int
}

// UpsertActor inserts a voice actor by anilist_person_id, updating name and
// image on conflict. Falls back to a name lookup if the insert fails due to
// the lower(name) unique index (actor was previously added manually).
func UpsertActor(ctx context.Context, pool *pgxpool.Pool, p ActorParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO actors (name, native_name, nationality, birthdate, profile_image_url, biography, anilist_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (anilist_person_id) DO UPDATE SET
			name              = EXCLUDED.name,
			native_name       = EXCLUDED.native_name,
			nationality       = COALESCE(actors.nationality, EXCLUDED.nationality),
			profile_image_url = EXCLUDED.profile_image_url,
			biography         = EXCLUDED.biography
		RETURNING id::text`,
		p.Name, p.NativeName, p.Nationality, p.Birthdate, p.ProfileImageURL, p.Biography, p.AnilistPersonID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}

	// Fallback: actor already exists by name (manual entry without anilist_person_id)
	if err2 := pool.QueryRow(ctx,
		"SELECT id::text FROM actors WHERE lower(name) = lower($1)", p.Name,
	).Scan(&id); err2 == nil {
		return id, nil
	}

	return "", err
}

func InsertCastMember(ctx context.Context, pool *pgxpool.Pool, catalogID, actorID, characterName, role string, sortOrder int) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO cast_members (catalog_id, actor_id, character_name, role, sort_order)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (catalog_id, actor_id) DO NOTHING`,
		catalogID, actorID, characterName, role, sortOrder,
	)
	return err
}
