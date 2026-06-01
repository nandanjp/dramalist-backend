package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

const actorCacheTTL = 30 * time.Minute
const actorCacheVersion = "v3:" // bump when actorDetailResponse shape changes

// ── Domain types ──────────────────────────────────────────────────────────────

type actorResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	NativeName      *string   `json:"native_name"`
	Birthdate       *string   `json:"birthdate"`
	Nationality     *string   `json:"nationality"`
	Biography       *string   `json:"biography"`
	ProfileImageURL *string   `json:"profile_image_url"`
	MDLPersonID     *int      `json:"mdl_person_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type actorFilmographyEntry struct {
	CastID        string   `json:"cast_id"`
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

// SearchActors returns actors matching a name prefix, or all actors (up to 100) when q is empty.
// Supports ?sort=name_asc|name_desc|created_at_desc|created_at_asc (default: name_asc).
// Response is always {"actors": [...]}.
// GET /actors?q=<name>&sort=<sort>
func (h *Handler) SearchActors(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	ctx := c.Request.Context()

	orderBy := "name ASC"
	switch c.Query("sort") {
	case "name_desc":
		orderBy = "name DESC"
	case "created_at_desc":
		orderBy = "created_at DESC"
	case "created_at_asc":
		orderBy = "created_at ASC"
	}

	var rows interface {
		Next() bool
		Scan(...any) error
		Close()
		Err() error
	}
	var err error

	if q == "" {
		rows, err = h.pool.Query(ctx,
			`SELECT id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, mdl_person_id, created_at, updated_at
			 FROM actors ORDER BY `+orderBy+` LIMIT 100`,
		)
	} else {
		rows, err = h.pool.Query(ctx,
			`SELECT id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, mdl_person_id, created_at, updated_at
			 FROM actors WHERE lower(name) LIKE lower($1) ORDER BY `+orderBy+` LIMIT 50`,
			q+"%",
		)
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	actors := make([]actorResponse, 0)
	for rows.Next() {
		var a actorResponse
		if err := rows.Scan(&a.ID, &a.Name, &a.NativeName, &a.Birthdate, &a.Nationality, &a.Biography, &a.ProfileImageURL, &a.MDLPersonID, &a.CreatedAt, &a.UpdatedAt); err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		actors = append(actors, a)
	}
	c.JSON(http.StatusOK, gin.H{"actors": actors})
}

// GetActorProfile returns a full actor profile with filmography.
// GET /actors/:id
func (h *Handler) GetActorProfile(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()
	cacheKey := actorCacheVersion + "actor:" + id

	if h.rdb != nil {
		if cached, err := h.rdb.Get(ctx, cacheKey).Result(); err == nil {
			var resp actorDetailResponse
			if json.Unmarshal([]byte(cached), &resp) == nil {
				c.JSON(http.StatusOK, resp)
				return
			}
		}
	}

	row, err := h.querier.GetActorDetail(ctx, id)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	if row == nil {
		errJSON(c, http.StatusNotFound, "actor not found")
		return
	}

	filmography := make([]actorFilmographyEntry, 0, len(row.Filmography))
	for _, f := range row.Filmography {
		filmography = append(filmography, actorFilmographyEntry{
			CastID:        f.CastID,
			CatalogID:     f.CatalogID,
			MediaType:     f.MediaType,
			Title:         f.Title,
			OriginalTitle: f.OriginalTitle,
			PosterURL:     f.PosterURL,
			Year:          f.Year,
			CharacterName: f.CharacterName,
			Role:          f.Role,
			SortOrder:     f.SortOrder,
		})
	}

	resp := actorDetailResponse{
		actorResponse: actorResponse{
			ID:              row.ID,
			Name:            row.Name,
			NativeName:      row.NativeName,
			Birthdate:       row.Birthdate,
			Nationality:     row.Nationality,
			Biography:       row.Biography,
			ProfileImageURL: row.ProfileImageURL,
			MDLPersonID:     row.MDLPersonID,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
		},
		Filmography: filmography,
	}

	if h.rdb != nil {
		if b, err := json.Marshal(resp); err == nil {
			h.rdb.Set(ctx, cacheKey, b, actorCacheTTL)
		}
	}

	c.JSON(http.StatusOK, resp)
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
		 RETURNING id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, mdl_person_id, created_at, updated_at`,
		req.Name, req.NativeName, req.Birthdate, req.Nationality, req.Biography, req.ProfileImageURL,
	).Scan(&a.ID, &a.Name, &a.NativeName, &a.Birthdate, &a.Nationality, &a.Biography, &a.ProfileImageURL, &a.MDLPersonID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "insert failed")
		return
	}
	c.JSON(http.StatusCreated, a)
}

// DeleteActor permanently removes an actor and their cast credits. Admin only.
// DELETE /actors/:id
func (h *Handler) DeleteActor(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	id := c.Param("id")
	ctx := c.Request.Context()

	tag, err := h.pool.Exec(ctx, `DELETE FROM actors WHERE id = $1`, id)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		errJSON(c, http.StatusNotFound, "actor not found")
		return
	}

	if h.rdb != nil {
		h.rdb.Del(context.Background(), actorCacheVersion+"actor:"+id)
	}

	c.Status(http.StatusNoContent)
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
		 RETURNING id::text, name, native_name, birthdate::text, nationality, biography, profile_image_url, mdl_person_id, created_at, updated_at`,
		req.Name, req.NativeName, req.Birthdate, req.Nationality, req.Biography, req.ProfileImageURL, id,
	).Scan(&a.ID, &a.Name, &a.NativeName, &a.Birthdate, &a.Nationality, &a.Biography, &a.ProfileImageURL, &a.MDLPersonID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			errJSON(c, http.StatusNotFound, "actor not found")
			return
		}
		errJSON(c, http.StatusInternalServerError, "update failed")
		return
	}

	if h.rdb != nil {
		h.rdb.Del(context.Background(), actorCacheVersion+"actor:"+id)
	}

	c.JSON(http.StatusOK, a)
}
