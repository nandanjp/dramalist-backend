package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	dramadb "dramalist/drama-service/db"
	"dramalist/drama-service/mdl"
)

type importRequest struct {
	MDLID         int    `json:"mdl_id"          binding:"required"`
	Slug          string `json:"slug"`            // full MDL slug (e.g. "781538-my-drama"); falls back to numeric ID string if empty
	CastMemberIDs []int  `json:"cast_member_ids"`
}

func (h *Handler) Import(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}

	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req importRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "mdl_id is required")
		return
	}

	ctx := c.Request.Context()

	exists, err := dramadb.CheckMDLExists(ctx, h.pool, req.MDLID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "database error")
		return
	}
	if exists {
		errJSON(c, http.StatusConflict, "already imported")
		return
	}

	slug := req.Slug
	if slug == "" {
		slug = strconv.Itoa(req.MDLID)
	}
	detail, err := h.client.FetchDetail(ctx, slug)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "MDL fetch failed")
		return
	}

	lang := mdl.CountryToLanguage(detail.Country)
	country := &detail.Country
	if detail.Country == "" {
		country = nil
	}

	params := dramadb.CatalogInsertParams{
		MediaType:       mdl.TypeToMediaType(detail.Type),
		Title:           detail.Title,
		OriginalTitle:   detail.OriginalTitle,
		Synopsis:        detail.Synopsis,
		PosterURL:       detail.PosterURL,
		Year:            detail.Year,
		Country:         country,
		Language:        lang,
		EpisodeCount:    detail.EpisodeCount,
		DurationMinutes: detail.Duration,
		Genre:           mdl.LowercaseGenres(detail.Genres),
		AiringStatus:    mdl.StatusToAiringStatus(detail.Status),
		MDLID:           detail.MDLID,
		CreatedBy:       userID,
	}

	catalogID, err := dramadb.InsertCatalog(ctx, h.pool, params)
	if err != nil {
		slog.Error("insert catalog failed", "err", err, "mdl_id", req.MDLID)
		errJSON(c, http.StatusInternalServerError, "failed to create catalog entry")
		return
	}

	allowedCast := make(map[int]bool, len(req.CastMemberIDs))
	for _, id := range req.CastMemberIDs {
		allowedCast[id] = true
	}

	castImported := 0
	for i, cm := range detail.Cast {
		if cm.PersonID == 0 || cm.Name == "" {
			continue
		}
		if len(allowedCast) > 0 && !allowedCast[cm.PersonID] {
			continue
		}

		actorParams := dramadb.ActorParams{
			Name:            cm.Name,
			ProfileImageURL: strPtrIfNonEmpty(cm.PhotoURL),
			MDLPersonID:     cm.PersonID,
		}

		actorID, err := dramadb.UpsertActor(ctx, h.pool, actorParams)
		if err != nil {
			slog.Warn("upsert actor failed, skipping", "actor", cm.Name, "err", err)
			continue
		}

		role := mdl.MapCastRole(cm.Role)
		if err := dramadb.InsertCastMember(ctx, h.pool, catalogID, actorID, cm.CharacterName, role, i); err != nil {
			slog.Warn("insert cast member failed, skipping", "character", cm.CharacterName, "err", err)
			continue
		}
		castImported++
	}

	c.JSON(http.StatusCreated, gin.H{
		"catalog_id":    catalogID,
		"title":         detail.Title,
		"mdl_id":        detail.MDLID,
		"cast_imported": castImported,
	})
}

func strPtrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
