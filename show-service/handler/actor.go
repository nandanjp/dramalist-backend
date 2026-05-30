package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// ── Domain types ──────────────────────────────────────────────────────────────

type actorResponse struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	NativeName       *string    `json:"native_name"`
	Birthdate        *string    `json:"birthdate"`
	Nationality      *string    `json:"nationality"`
	Biography        *string    `json:"biography"`
	ProfileImageURL  *string    `json:"profile_image_url"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type actorFilmographyEntry struct {
	CatalogID     string   `json:"catalog_id"`
	MediaType     string   `json:"media_type"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title"`
	PosterURL     *string  `json:"poster_url"`
	Year          *int     `json:"year"`
	CharacterName *string  `json:"character_name"`
	Role          string   `json:"role"`
	SortOrder     int      `json:"sort_order"`
}

type actorDetailResponse struct {
	actorResponse
	Filmography []actorFilmographyEntry `json:"filmography"`
}

type createActorRequest struct {
	Name            string  `json:"name" binding:"required"`
	NativeName      *string `json:"native_name"`
	Birthdate       *string `json:"birthdate"`
	Nationality     *string `json:"nationality"`
	Biography       *string `json:"biography"`
	ProfileImageURL *string `json:"profile_image_url"`
}

type patchActorRequest struct {
	Name            *string `json:"name"`
	NativeName      *string `json:"native_name"`
	Birthdate       *string `json:"birthdate"`
	Nationality     *string `json:"nationality"`
	Biography       *string `json:"biography"`
	ProfileImageURL *string `json:"profile_image_url"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// SearchActors returns actors matching a name prefix query.
// GET /actors?q=<name>
func (h *Handler) SearchActors(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusOK, []actorResponse{})
		return
	}
	ctx := c.Request.Context()
	rows, err := h.pool.Query(ctx,
		`SELECT id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, created_at, updated_at
		 FROM actors WHERE lower(name) LIKE lower($1) ORDER BY name LIMIT 20`,
		q+"%",
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	actors := make([]actorResponse, 0)
	for rows.Next() {
		var a actorResponse
		if err := rows.Scan(&a.ID, &a.Name, &a.NativeName, &a.Birthdate, &a.Nationality, &a.Biography, &a.ProfileImageURL, &a.CreatedAt, &a.UpdatedAt); err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		actors = append(actors, a)
	}
	c.JSON(http.StatusOK, actors)
}

// GetActorProfile returns a full actor profile with filmography.
// GET /actors/:id
func (h *Handler) GetActorProfile(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	var a actorResponse
	if err := h.pool.QueryRow(ctx,
		`SELECT id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, created_at, updated_at
		 FROM actors WHERE id = $1`, id,
	).Scan(&a.ID, &a.Name, &a.NativeName, &a.Birthdate, &a.Nationality, &a.Biography, &a.ProfileImageURL, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			errJSON(c, http.StatusNotFound, "actor not found")
			return
		}
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	rows, err := h.pool.Query(ctx,
		`SELECT c.id::text, c.media_type, c.title, c.original_title, c.poster_url, c.year,
		        cm.character_name, cm.role, cm.sort_order
		 FROM cast_members cm
		 JOIN catalog c ON c.id = cm.catalog_id
		 WHERE cm.actor_id = $1
		 ORDER BY c.year DESC NULLS LAST, c.title ASC`,
		id,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "filmography query failed")
		return
	}
	defer rows.Close()

	filmography := make([]actorFilmographyEntry, 0)
	for rows.Next() {
		var f actorFilmographyEntry
		if err := rows.Scan(&f.CatalogID, &f.MediaType, &f.Title, &f.OriginalTitle, &f.PosterURL, &f.Year, &f.CharacterName, &f.Role, &f.SortOrder); err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		filmography = append(filmography, f)
	}

	c.JSON(http.StatusOK, actorDetailResponse{
		actorResponse: a,
		Filmography:   filmography,
	})
}

// CreateActor creates a new global actor (upsert by normalized name). Admin only.
// POST /actors
func (h *Handler) CreateActor(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}

	var req createActorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "name is required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		errJSON(c, http.StatusBadRequest, "name is required")
		return
	}

	ctx := c.Request.Context()
	var a actorResponse
	err := h.pool.QueryRow(ctx,
		`INSERT INTO actors (name, native_name, birthdate, nationality, biography, profile_image_url)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (lower(name)) DO UPDATE
		   SET name = EXCLUDED.name,
		       native_name = COALESCE(EXCLUDED.native_name, actors.native_name),
		       nationality = COALESCE(EXCLUDED.nationality, actors.nationality),
		       biography = COALESCE(EXCLUDED.biography, actors.biography),
		       profile_image_url = COALESCE(EXCLUDED.profile_image_url, actors.profile_image_url),
		       updated_at = NOW()
		 RETURNING id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, created_at, updated_at`,
		req.Name, req.NativeName, req.Birthdate, req.Nationality, req.Biography, req.ProfileImageURL,
	).Scan(&a.ID, &a.Name, &a.NativeName, &a.Birthdate, &a.Nationality, &a.Biography, &a.ProfileImageURL, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "insert failed")
		return
	}
	c.JSON(http.StatusCreated, a)
}

// UpdateActor patches an actor's profile. Admin only.
// PATCH /actors/:id
func (h *Handler) UpdateActor(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	id := c.Param("id")

	var req patchActorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request")
		return
	}

	ctx := c.Request.Context()
	var a actorResponse
	err := h.pool.QueryRow(ctx,
		`UPDATE actors
		 SET name             = COALESCE($1, name),
		     native_name      = COALESCE($2, native_name),
		     birthdate        = COALESCE($3::date, birthdate),
		     nationality      = COALESCE($4, nationality),
		     biography        = COALESCE($5, biography),
		     profile_image_url = COALESCE($6, profile_image_url),
		     updated_at       = NOW()
		 WHERE id = $7
		 RETURNING id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, created_at, updated_at`,
		req.Name, req.NativeName, req.Birthdate, req.Nationality, req.Biography, req.ProfileImageURL, id,
	).Scan(&a.ID, &a.Name, &a.NativeName, &a.Birthdate, &a.Nationality, &a.Biography, &a.ProfileImageURL, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			errJSON(c, http.StatusNotFound, "actor not found")
			return
		}
		errJSON(c, http.StatusInternalServerError, "update failed")
		return
	}
	c.JSON(http.StatusOK, a)
}
