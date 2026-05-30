package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"dramalist/review-service/kafka"
	"dramalist/review-service/markdown"
)

const aggregateCacheTTL = 5 * time.Minute

// ── Response types ────────────────────────────────────────────────────────────

const maxContentLen = 10_000

type reviewResponse struct {
	ID               string    `json:"id"`
	CatalogID        string    `json:"catalog_id"`
	CatalogTitle     *string   `json:"catalog_title"`
	UserID           string    `json:"user_id"`
	Rating           float64   `json:"rating"`
	Content          *string   `json:"content"`
	ContentHTML      *string   `json:"content_html"`
	ContainsSpoilers bool      `json:"contains_spoilers"`
	IsPublic         bool      `json:"is_public"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type aggregateResponse struct {
	CatalogID   string   `json:"catalog_id"`
	AvgRating   *float64 `json:"avg_rating"`
	ReviewCount int      `json:"review_count"`
}

type listResponse struct {
	Reviews []reviewResponse `json:"reviews"`
	Total   int64            `json:"total"`
	Page    int              `json:"page"`
	Limit   int              `json:"limit"`
}

// ── Request types ─────────────────────────────────────────────────────────────

type createReviewRequest struct {
	CatalogID        string   `json:"catalog_id"  binding:"required"`
	CatalogTitle     *string  `json:"catalog_title"`
	Rating           *float64 `json:"rating"`
	Content          *string  `json:"content"`
	ContainsSpoilers bool     `json:"contains_spoilers"`
	IsPublic         *bool    `json:"is_public"`
	// Forwarded into review.created Kafka event so user-service can update
	// watch_stats without calling show-service. Client has this data already.
	ShowGenres       []string `json:"show_genres"`
	ShowEpisodeCount int      `json:"show_episode_count"`
}

type patchReviewRequest struct {
	Rating           *float64 `json:"rating"`
	Content          *string  `json:"content"`
	ContainsSpoilers *bool    `json:"contains_spoilers"`
	IsPublic         *bool    `json:"is_public"`
}

// ── POST /reviews ─────────────────────────────────────────────────────────────

func (h *Handler) CreateReview(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req createReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Rating == nil || *req.Rating < 0 || *req.Rating > 10 {
		errJSON(c, http.StatusBadRequest, "rating is required and must be between 0 and 10")
		return
	}
	if req.Content != nil && len(*req.Content) > maxContentLen {
		errJSON(c, http.StatusBadRequest, "content must be 10,000 characters or fewer")
		return
	}

	isPublic := true
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	}
	if req.ShowGenres == nil {
		req.ShowGenres = []string{}
	}

	ctx := c.Request.Context()

	var reviewID string
	err := h.pool.QueryRow(ctx,
		`INSERT INTO reviews (catalog_id, catalog_title, user_id, rating, content, contains_spoilers, is_public)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id::text`,
		req.CatalogID, req.CatalogTitle, userID, *req.Rating, req.Content, req.ContainsSpoilers, isPublic,
	).Scan(&reviewID)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			errJSON(c, http.StatusConflict, "you already have a review for this title")
			return
		}
		if strings.Contains(err.Error(), "invalid input syntax for type uuid") {
			errJSON(c, http.StatusBadRequest, "invalid catalog_id")
			return
		}
		errJSON(c, http.StatusInternalServerError, "create failed")
		return
	}

	if err := h.recomputeAggregate(ctx, req.CatalogID); err != nil {
		slog.Error("aggregate recompute failed", "catalog_id", req.CatalogID, "err", err)
	}

	go h.producer.Publish(context.Background(), kafka.ReviewEvent{
		Event:            "review.created",
		ReviewID:         reviewID,
		UserID:           userID,
		CatalogID:        req.CatalogID,
		Rating:           *req.Rating,
		ShowGenres:       req.ShowGenres,
		ShowEpisodeCount: req.ShowEpisodeCount,
	})

	review, err := h.fetchReview(ctx, reviewID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}
	c.JSON(http.StatusCreated, review)
}

// ── GET /reviews/catalog/:catalogId ──────────────────────────────────────────

func (h *Handler) ListShowReviews(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	catalogID := c.Param("catalogId")
	page, limit := parsePagination(c)
	ctx := c.Request.Context()

	// Own review always visible; other users' reviews only if is_public.
	var total int64
	if err := h.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM reviews
		 WHERE catalog_id = $1 AND (is_public = true OR user_id = $2)`,
		catalogID, userID,
	).Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	rows, err := h.pool.Query(ctx,
		`SELECT id::text, catalog_id::text, catalog_title, user_id::text, rating, content,
		        contains_spoilers, is_public, created_at, updated_at
		 FROM reviews
		 WHERE catalog_id = $1 AND (is_public = true OR user_id = $2)
		 ORDER BY created_at DESC
		 LIMIT $3 OFFSET $4`,
		catalogID, userID, limit, (page-1)*limit,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	reviews := make([]reviewResponse, 0)
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		reviews = append(reviews, r)
	}
	c.JSON(http.StatusOK, listResponse{Reviews: reviews, Total: total, Page: page, Limit: limit})
}

// ── GET /reviews/aggregate/:catalogId ────────────────────────────────────────

func (h *Handler) GetAggregate(c *gin.Context) {
	catalogID := c.Param("catalogId")
	ctx := c.Request.Context()

	cacheKey := "review_agg:" + catalogID

	// Try Redis cache first.
	cached, err := h.rdb.Get(ctx, cacheKey).Bytes()
	if err == nil {
		var agg aggregateResponse
		if json.Unmarshal(cached, &agg) == nil {
			c.JSON(http.StatusOK, agg)
			return
		}
	}

	var agg aggregateResponse
	agg.CatalogID = catalogID

	err = h.pool.QueryRow(ctx,
		`SELECT avg_rating, review_count FROM review_aggregates WHERE catalog_id = $1`,
		catalogID,
	).Scan(&agg.AvgRating, &agg.ReviewCount)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusOK, agg)
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "aggregate fetch failed")
		return
	}

	if payload, err := json.Marshal(agg); err == nil {
		h.rdb.Set(ctx, cacheKey, payload, aggregateCacheTTL)
	}
	c.JSON(http.StatusOK, agg)
}

// ── GET /reviews/me ───────────────────────────────────────────────────────────

func (h *Handler) ListMyReviews(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}
	page, limit := parsePagination(c)
	ctx := c.Request.Context()

	var total int64
	if err := h.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM reviews WHERE user_id = $1", userID,
	).Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	rows, err := h.pool.Query(ctx,
		`SELECT id::text, catalog_id::text, catalog_title, user_id::text, rating, content,
		        contains_spoilers, is_public, created_at, updated_at
		 FROM reviews WHERE user_id = $1
		 ORDER BY updated_at DESC
		 LIMIT $2 OFFSET $3`,
		userID, limit, (page-1)*limit,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	reviews := make([]reviewResponse, 0)
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		reviews = append(reviews, r)
	}
	c.JSON(http.StatusOK, listResponse{Reviews: reviews, Total: total, Page: page, Limit: limit})
}

// ── GET /reviews/:id ──────────────────────────────────────────────────────────

func (h *Handler) GetReview(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	reviewID := c.Param("id")
	ctx := c.Request.Context()

	review, err := h.fetchReview(ctx, reviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "review not found")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}
	if review.UserID != userID && !review.IsPublic {
		errJSON(c, http.StatusNotFound, "review not found")
		return
	}
	c.JSON(http.StatusOK, review)
}

// ── PATCH /reviews/:id ────────────────────────────────────────────────────────

func (h *Handler) UpdateReview(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}
	reviewID := c.Param("id")

	var req patchReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Rating != nil && (*req.Rating < 0 || *req.Rating > 10) {
		errJSON(c, http.StatusBadRequest, "rating must be between 0 and 10")
		return
	}
	if req.Content != nil && len(*req.Content) > maxContentLen {
		errJSON(c, http.StatusBadRequest, "content must be 10,000 characters or fewer")
		return
	}

	ctx := c.Request.Context()

	var ownerID, catalogID string
	var currentRating float64
	err := h.pool.QueryRow(ctx,
		"SELECT user_id::text, catalog_id::text, rating FROM reviews WHERE id = $1", reviewID,
	).Scan(&ownerID, &catalogID, &currentRating)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "review not found")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}
	if ownerID != userID {
		errJSON(c, http.StatusForbidden, "not your review")
		return
	}

	cols := []string{}
	args := []any{reviewID}
	n := 2

	if req.Rating != nil {
		cols = append(cols, fmt.Sprintf("rating = $%d", n)); args = append(args, *req.Rating); n++
	}
	if req.Content != nil {
		cols = append(cols, fmt.Sprintf("content = $%d", n)); args = append(args, *req.Content); n++
	}
	if req.ContainsSpoilers != nil {
		cols = append(cols, fmt.Sprintf("contains_spoilers = $%d", n)); args = append(args, *req.ContainsSpoilers); n++
	}
	if req.IsPublic != nil {
		cols = append(cols, fmt.Sprintf("is_public = $%d", n)); args = append(args, *req.IsPublic); n++
	}

	if len(cols) > 0 {
		cols = append(cols, "updated_at = NOW()")
		query := "UPDATE reviews SET " + strings.Join(cols, ", ") + " WHERE id = $1"
		if _, err := h.pool.Exec(ctx, query, args...); err != nil {
			errJSON(c, http.StatusInternalServerError, "update failed")
			return
		}
		if err := h.recomputeAggregate(ctx, catalogID); err != nil {
			slog.Error("aggregate recompute failed", "catalog_id", catalogID, "err", err)
		}
	}

	review, err := h.fetchReview(ctx, reviewID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}

	if req.Rating != nil && *req.Rating != currentRating {
		go h.producer.Publish(context.Background(), kafka.ReviewEvent{
			Event:     "review.updated",
			ReviewID:  reviewID,
			UserID:    userID,
			CatalogID: catalogID,
			Rating:    review.Rating,
			OldRating: &currentRating,
		})
	}

	c.JSON(http.StatusOK, review)
}

// ── DELETE /reviews/:id ───────────────────────────────────────────────────────

func (h *Handler) DeleteReview(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}
	reviewID := c.Param("id")
	ctx := c.Request.Context()

	var catalogID string
	var rating float64
	err := h.pool.QueryRow(ctx,
		"SELECT catalog_id::text, rating FROM reviews WHERE id = $1 AND user_id = $2",
		reviewID, userID,
	).Scan(&catalogID, &rating)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "review not found")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}

	if _, err := h.pool.Exec(ctx, "DELETE FROM reviews WHERE id = $1", reviewID); err != nil {
		errJSON(c, http.StatusInternalServerError, "delete failed")
		return
	}

	if err := h.recomputeAggregate(ctx, catalogID); err != nil {
		slog.Error("aggregate recompute failed", "catalog_id", catalogID, "err", err)
	}

	go h.producer.Publish(context.Background(), kafka.ReviewEvent{
		Event:     "review.deleted",
		ReviewID:  reviewID,
		UserID:    userID,
		CatalogID: catalogID,
		Rating:    rating,
	})

	c.Status(http.StatusNoContent)
}

// ── Shared helpers ────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanReview(row scanner) (reviewResponse, error) {
	var r reviewResponse
	err := row.Scan(
		&r.ID, &r.CatalogID, &r.CatalogTitle, &r.UserID, &r.Rating, &r.Content,
		&r.ContainsSpoilers, &r.IsPublic, &r.CreatedAt, &r.UpdatedAt,
	)
	if err == nil && r.Content != nil {
		rendered := markdown.Render(*r.Content)
		r.ContentHTML = &rendered
	}
	return r, err
}

func (h *Handler) fetchReview(ctx context.Context, reviewID string) (reviewResponse, error) {
	row := h.pool.QueryRow(ctx,
		`SELECT id::text, catalog_id::text, catalog_title, user_id::text, rating, content,
		        contains_spoilers, is_public, created_at, updated_at
		 FROM reviews WHERE id = $1`, reviewID)
	return scanReview(row)
}

func (h *Handler) recomputeAggregate(ctx context.Context, catalogID string) error {
	_, err := h.pool.Exec(ctx,
		`INSERT INTO review_aggregates (catalog_id, avg_rating, review_count)
		 VALUES (
		     $1,
		     (SELECT ROUND(AVG(rating)::numeric, 1) FROM reviews WHERE catalog_id = $1),
		     (SELECT COUNT(*) FROM reviews WHERE catalog_id = $1)
		 )
		 ON CONFLICT (catalog_id) DO UPDATE
		     SET avg_rating   = EXCLUDED.avg_rating,
		         review_count = EXCLUDED.review_count,
		         updated_at   = NOW()`,
		catalogID,
	)
	if err != nil {
		return err
	}
	h.rdb.Del(ctx, "review_agg:"+catalogID)
	return nil
}

func parsePagination(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return
}
