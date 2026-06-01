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
	ProfileImageURL *string
	MDLPersonID     int
}

func UpsertActor(ctx context.Context, pool *pgxpool.Pool, p ActorParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO actors (name, native_name, profile_image_url, mdl_person_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (mdl_person_id) DO UPDATE
		  SET name              = EXCLUDED.name,
		      profile_image_url = EXCLUDED.profile_image_url
		RETURNING id::text`,
		p.Name, p.NativeName, p.ProfileImageURL, p.MDLPersonID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}

	// If mdl_person_id uniqueness conflicts with a manually-added actor row
	// that has no mdl_person_id, fall back to a name lookup.
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
