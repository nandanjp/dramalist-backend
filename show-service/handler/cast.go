package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ── Domain types ──────────────────────────────────────────────────────────────

type castMemberResponse struct {
	CastID          string  `json:"cast_id"`
	ActorID         string  `json:"actor_id"`
	ActorName       string  `json:"actor_name"`
	ProfileImageURL *string `json:"profile_image_url"`
	CharacterName   *string `json:"character_name"`
	Role            string  `json:"role"`
	SortOrder       int     `json:"sort_order"`
}

type addCastMemberRequest struct {
	ActorID       string  `json:"actor_id" binding:"required"`
	CharacterName *string `json:"character_name"`
	Role          string  `json:"role"`
	SortOrder     int     `json:"sort_order"`
}

type patchCastMemberRequest struct {
	CharacterName *string `json:"character_name"`
	Role          *string `json:"role"`
	SortOrder     *int    `json:"sort_order"`
}

var validRoles = map[string]bool{
	"main":       true,
	"supporting": true,
	"guest":      true,
}

var errNotOwner = errors.New("not owner")

// ── Helper ────────────────────────────────────────────────────────────────────

func (h *Handler) fetchCast(ctx context.Context, catalogID string) ([]castMemberResponse, error) {
	rows, err := h.pool.Query(ctx,
		`SELECT cm.id::text, cm.actor_id::text, a.name, a.profile_image_url, cm.character_name, cm.role, cm.sort_order
		 FROM cast_members cm
		 JOIN actors a ON a.id = cm.actor_id
		 WHERE cm.catalog_id = $1
		 ORDER BY cm.sort_order, a.name`,
		catalogID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cast := make([]castMemberResponse, 0)
	for rows.Next() {
		var m castMemberResponse
		if err := rows.Scan(&m.CastID, &m.ActorID, &m.ActorName, &m.ProfileImageURL, &m.CharacterName, &m.Role, &m.SortOrder); err != nil {
			return nil, err
		}
		cast = append(cast, m)
	}
	return cast, nil
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GetCast returns the cast for a catalog entry.
// GET /catalog/:id/cast
func (h *Handler) GetCast(c *gin.Context) {
	catalogID := c.Param("id")
	ctx := c.Request.Context()

	cast, err := h.fetchCast(ctx, catalogID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	c.JSON(http.StatusOK, cast)
}

// AddCastMember adds an actor to a catalog entry's cast. Admin only.
// POST /catalog/:id/cast
func (h *Handler) AddCastMember(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	catalogID := c.Param("id")

	var req addCastMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "actor_id is required")
		return
	}
	if req.Role == "" {
		req.Role = "supporting"
	}
	if !validRoles[req.Role] {
		errJSON(c, http.StatusBadRequest, "role must be main, supporting, or guest")
		return
	}

	ctx := c.Request.Context()
	var m castMemberResponse
	err := h.pool.QueryRow(ctx,
		`INSERT INTO cast_members (catalog_id, actor_id, character_name, role, sort_order)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id::text, actor_id::text, character_name, role, sort_order`,
		catalogID, req.ActorID, req.CharacterName, req.Role, req.SortOrder,
	).Scan(&m.CastID, &m.ActorID, &m.CharacterName, &m.Role, &m.SortOrder)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505": // unique_violation
				errJSON(c, http.StatusConflict, "actor already in cast for this title")
				return
			case "23503": // foreign_key_violation
				errJSON(c, http.StatusNotFound, "catalog entry or actor not found")
				return
			}
		}
		errJSON(c, http.StatusInternalServerError, "insert failed")
		return
	}

	h.pool.QueryRow(ctx, `SELECT name FROM actors WHERE id = $1`, req.ActorID).Scan(&m.ActorName) //nolint:errcheck
	if h.rdb != nil {
		h.rdb.Del(ctx, actorCacheVersion+"actor:"+req.ActorID)
	}
	invalidateCatalogCache(h, catalogID)
	c.JSON(http.StatusCreated, m)
}

// UpdateCastMember patches a cast entry. Admin only.
// PATCH /catalog/:id/cast/:castId
func (h *Handler) UpdateCastMember(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	catalogID := c.Param("id")
	castID := c.Param("castId")

	var req patchCastMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Role != nil && !validRoles[*req.Role] {
		errJSON(c, http.StatusBadRequest, "role must be main, supporting, or guest")
		return
	}

	ctx := c.Request.Context()
	result, err := h.pool.Exec(ctx,
		`UPDATE cast_members
		 SET character_name = COALESCE($1, character_name),
		     role            = COALESCE($2, role),
		     sort_order      = COALESCE($3, sort_order)
		 WHERE id = $4 AND catalog_id = $5`,
		req.CharacterName, req.Role, req.SortOrder, castID, catalogID,
	)
	if err != nil || result.RowsAffected() == 0 {
		errJSON(c, http.StatusNotFound, "cast member not found")
		return
	}
	c.Status(http.StatusNoContent)
}

// RemoveCastMember removes an actor from a catalog entry's cast. Admin only.
// DELETE /catalog/:id/cast/:castId
func (h *Handler) RemoveCastMember(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	catalogID := c.Param("id")
	castID := c.Param("castId")
	ctx := c.Request.Context()

	var actorID string
	err := h.pool.QueryRow(ctx,
		`DELETE FROM cast_members WHERE id = $1 AND catalog_id = $2 RETURNING actor_id::text`,
		castID, catalogID,
	).Scan(&actorID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			errJSON(c, http.StatusNotFound, "cast member not found")
			return
		}
		errJSON(c, http.StatusInternalServerError, "delete failed")
		return
	}
	if h.rdb != nil {
		h.rdb.Del(ctx, actorCacheVersion+"actor:"+actorID)
	}
	invalidateCatalogCache(h, catalogID)
	c.Status(http.StatusNoContent)
}
