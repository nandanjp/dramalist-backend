package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	dramadb "dramalist/drama-service/db"
)

const showActorCacheVersion = "v3:"

func (h *Handler) invalidateActorCache(actorID string) {
	if h.rdb == nil {
		return
	}
	bg := context.Background()
	h.rdb.Del(bg, showActorCacheVersion+"actor:"+actorID)
	h.rdb.Incr(bg, showActorCacheVersion+"actors:list:ver")
}

type personImportRequest struct {
	TMDBPersonID int `json:"tmdb_person_id" binding:"required"`
}

type syncImageRequest struct {
	ActorID      string `json:"actor_id"       binding:"required"`
	TMDBPersonID int    `json:"tmdb_person_id" binding:"required"`
}

func (h *Handler) PersonPreview(c *gin.Context) {
	idStr := c.Param("id")
	personID, err := strconv.Atoi(idStr)
	if err != nil || personID <= 0 {
		errJSON(c, http.StatusBadRequest, "id must be a positive integer")
		return
	}

	person, err := h.tmdb.FetchPerson(c.Request.Context(), personID)
	if err != nil {
		slog.Warn("FetchPerson failed", "tmdb_person_id", personID, "err", err)
		errJSON(c, http.StatusBadGateway, "TMDB fetch failed")
		return
	}

	var nativeName *string
	if len(person.AlsoKnownAs) > 0 && person.AlsoKnownAs[0] != "" {
		nativeName = &person.AlsoKnownAs[0]
	}

	var profileURL *string
	if person.ProfilePath != "" {
		u := h.tmdb.ProfileURL(person.ProfilePath)
		profileURL = &u
	}

	var nationality *string
	if person.PlaceOfBirth != "" {
		parts := strings.Split(person.PlaceOfBirth, ",")
		n := strings.TrimSpace(parts[len(parts)-1])
		if n != "" {
			nationality = &n
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"tmdb_person_id": person.ID,
		"name":           person.Name,
		"native_name":    nativeName,
		"profile_url":    profileURL,
		"birthdate":      person.Birthday,
		"nationality":    nationality,
		"biography":      person.Biography,
	})
}

func (h *Handler) PersonImport(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}

	var req personImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "tmdb_person_id is required")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetHeader("X-User-Id")

	person, err := h.tmdb.FetchPerson(ctx, req.TMDBPersonID)
	if err != nil {
		slog.Warn("FetchPerson failed during import", "tmdb_person_id", req.TMDBPersonID, "err", err)
		errJSON(c, http.StatusBadGateway, "TMDB fetch failed")
		return
	}

	params := dramadb.ActorParams{
		Name:         person.Name,
		TMDBPersonID: person.ID,
	}
	if len(person.AlsoKnownAs) > 0 && person.AlsoKnownAs[0] != "" {
		params.NativeName = &person.AlsoKnownAs[0]
	}
	if person.Birthday != "" {
		params.Birthdate = &person.Birthday
	}
	if person.Biography != "" {
		params.Biography = &person.Biography
	}
	if person.PlaceOfBirth != "" {
		parts := strings.Split(person.PlaceOfBirth, ",")
		n := strings.TrimSpace(parts[len(parts)-1])
		if n != "" {
			params.Nationality = &n
		}
	}
	if person.ProfilePath != "" {
		u := h.tmdb.ProfileURL(person.ProfilePath)
		params.ProfileImageURL = &u
	}

	actorID, err := dramadb.UpsertActor(ctx, h.pool, params)
	if err != nil {
		slog.Error("upsert actor failed", "err", err, "tmdb_person_id", person.ID)
		errJSON(c, http.StatusInternalServerError, "failed to save actor")
		return
	}

	if person.ProfilePath != "" {
		imgURL := h.tmdb.ProfileURL(person.ProfilePath)
		if mirrored := h.mirrorImage(ctx, imgURL, "actor", actorID, "profile", userID); mirrored != "" {
			if err := dramadb.UpdateActorProfileImage(ctx, h.pool, actorID, mirrored); err != nil {
				slog.Warn("update actor profile image failed", "err", err)
			} else {
				h.invalidateActorCache(actorID)
			}
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"actor_id":       actorID,
		"name":           person.Name,
		"tmdb_person_id": person.ID,
	})
}

func (h *Handler) SyncActorImage(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}

	var req syncImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "actor_id and tmdb_person_id are required")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetHeader("X-User-Id")

	person, err := h.tmdb.FetchPerson(ctx, req.TMDBPersonID)
	if err != nil {
		slog.Warn("SyncActorImage: FetchPerson failed", "tmdb_person_id", req.TMDBPersonID, "err", err)
		errJSON(c, http.StatusBadGateway, "TMDB fetch failed")
		return
	}

	if person.ProfilePath == "" {
		errJSON(c, http.StatusUnprocessableEntity, "no profile image found on TMDB")
		return
	}

	imgURL := h.tmdb.ProfileURL(person.ProfilePath)
	mirrored := h.mirrorImage(ctx, imgURL, "actor", req.ActorID, "profile", userID)
	if mirrored == "" {
		errJSON(c, http.StatusUnprocessableEntity, "profile image unavailable")
		return
	}

	if err := dramadb.UpdateActorProfileImage(ctx, h.pool, req.ActorID, mirrored); err != nil {
		slog.Error("SyncActorImage: update profile image failed", "actor_id", req.ActorID, "err", err)
		errJSON(c, http.StatusInternalServerError, "failed to update actor")
		return
	}
	h.invalidateActorCache(req.ActorID)

	c.JSON(http.StatusOK, gin.H{
		"actor_id":          req.ActorID,
		"profile_image_url": mirrored,
	})
}
