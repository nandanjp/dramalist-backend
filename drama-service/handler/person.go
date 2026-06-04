package handler

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	dramadb "dramalist/drama-service/db"
)

type personImportRequest struct {
	PersonID int    `json:"person_id" binding:"required"`
	Slug     string `json:"slug"      binding:"required"`
}

type syncImageRequest struct {
	ActorID     string `json:"actor_id"      binding:"required"`
	MDLPersonID int    `json:"mdl_person_id" binding:"required"`
}

func (h *Handler) PersonPreview(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		errJSON(c, http.StatusBadRequest, "slug required")
		return
	}

	person, err := h.client.FetchPerson(c.Request.Context(), slug)
	if err != nil {
		slog.Warn("FetchPerson failed", "slug", slug, "err", err)
		errJSON(c, http.StatusBadGateway, "MDL fetch failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"person_id":   person.PersonID,
		"slug":        person.Slug,
		"name":        person.Name,
		"native_name": person.NativeName,
		"profile_url": person.ProfileURL,
		"birthdate":   person.Birthdate,
		"nationality": person.Nationality,
		"biography":   person.Biography,
	})
}

func (h *Handler) PersonImport(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}

	var req personImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "person_id and slug are required")
		return
	}

	ctx := c.Request.Context()

	person, err := h.client.FetchPerson(ctx, req.Slug)
	if err != nil {
		slog.Warn("FetchPerson failed during import", "slug", req.Slug, "err", err)
		errJSON(c, http.StatusBadGateway, "MDL fetch failed")
		return
	}

	userID := c.GetHeader("X-User-Id")

	profileURL := person.ProfileURL
	params := dramadb.ActorParams{
		Name:            person.Name,
		NativeName:      person.NativeName,
		Birthdate:       person.Birthdate,
		Nationality:     person.Nationality,
		Biography:       person.Biography,
		ProfileImageURL: profileURL,
		MDLPersonID:     person.PersonID,
	}

	actorID, err := dramadb.UpsertActor(ctx, h.pool, params)
	if err != nil {
		slog.Error("upsert actor failed", "err", err, "mdl_person_id", person.PersonID)
		errJSON(c, http.StatusInternalServerError, "failed to save actor")
		return
	}

	// Mirror profile image to MinIO and update the stored URL.
	if person.ProfileURL != nil && *person.ProfileURL != "" {
		if mirrored := h.mirrorImage(ctx, *person.ProfileURL, "actor", actorID, "profile", userID); mirrored != "" {
			if err := dramadb.UpdateActorProfileImage(ctx, h.pool, actorID, mirrored); err != nil {
				slog.Warn("update actor profile image failed", "err", err)
			}
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"actor_id":      actorID,
		"name":          person.Name,
		"mdl_person_id": person.PersonID,
	})
}

// SyncActorImage fetches the current MDL profile image for an existing actor,
// mirrors it to MinIO, and updates profile_image_url.
func (h *Handler) SyncActorImage(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}

	var req syncImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "actor_id and mdl_person_id are required")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetHeader("X-User-Id")

	// Fetch current profile image from MDL using just the numeric ID as slug;
	// MDL redirects /people/{id} to the canonical slug URL.
	slug := fmt.Sprintf("%d", req.MDLPersonID)
	person, err := h.client.FetchPerson(ctx, slug)
	if err != nil {
		slog.Warn("SyncActorImage: FetchPerson failed", "mdl_person_id", req.MDLPersonID, "err", err)
		errJSON(c, http.StatusBadGateway, "MDL fetch failed")
		return
	}

	if person.ProfileURL == nil || *person.ProfileURL == "" {
		errJSON(c, http.StatusUnprocessableEntity, "no profile image found on MDL")
		return
	}

	mirrored := h.mirrorImage(ctx, *person.ProfileURL, "actor", req.ActorID, "profile", userID)
	if mirrored == "" {
		errJSON(c, http.StatusBadGateway, "image mirror failed")
		return
	}

	if err := dramadb.UpdateActorProfileImage(ctx, h.pool, req.ActorID, mirrored); err != nil {
		slog.Error("SyncActorImage: update profile image failed", "actor_id", req.ActorID, "err", err)
		errJSON(c, http.StatusInternalServerError, "failed to update actor")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"actor_id":          req.ActorID,
		"profile_image_url": mirrored,
	})
}
