package db

import (
	"context"
	"time"
)

// ── Row types ─────────────────────────────────────────────────────────────────

type CatalogRow struct {
	ID              string
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
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CastMemberRow struct {
	CastID          string
	ActorID         string
	ActorName       string
	ProfileImageURL *string
	CharacterName   *string
	Role            string
	SortOrder       int
}

type CatalogDetailRow struct {
	CatalogRow
	Cast []CastMemberRow
}

type ActorRow struct {
	ID              string
	Name            string
	NativeName      *string
	Birthdate       *string
	Nationality     *string
	Biography       *string
	ProfileImageURL *string
	MDLPersonID     *int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type FilmographyRow struct {
	CastID        string
	CatalogID     string
	MediaType     string
	Title         string
	OriginalTitle *string
	PosterURL     *string
	Year          *int
	CharacterName *string
	Role          string
	SortOrder     int
}

type ActorDetailRow struct {
	ActorRow
	Filmography []FilmographyRow
}

// ── Querier interface ─────────────────────────────────────────────────────────
//
// Querier covers the cacheable read operations in show-service. The remaining
// handlers (catalog/actor CRUD, list entries, cast management, export) continue
// to use *pgxpool.Pool directly; they will be migrated to a full Store interface
// in a future iteration.

type Querier interface {
	// DiscoverCatalog returns catalog entries ordered by the given column expression
	// (safe: callers use hardcoded strings, never user input).
	DiscoverCatalog(ctx context.Context, orderBy string, limit int) ([]CatalogRow, error)
	// GetCatalogDetail returns a catalog entry with its full cast list.
	GetCatalogDetail(ctx context.Context, id string) (*CatalogDetailRow, error)
	// GetActorDetail returns an actor with their full filmography.
	GetActorDetail(ctx context.Context, id string) (*ActorDetailRow, error)
}
