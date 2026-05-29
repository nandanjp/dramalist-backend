package handler

import (
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"dramalist/media-service/storage"
)

var allowedMIME = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/gif":  "gif",
	"image/webp": "webp",
}

var allowedEntityTypes = map[string]bool{"show": true, "user": true}
var allowedMediaTypes = map[string]bool{"poster": true, "banner": true, "thumbnail": true, "avatar": true}

type mediaRecord struct {
	ID          string `json:"id"`
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	MediaType   string `json:"media_type"`
	URL         string `json:"url"`
	Width       *int   `json:"width,omitempty"`
	Height      *int   `json:"height,omitempty"`
	SizeBytes   *int64 `json:"size_bytes,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// Upload accepts a multipart file and stores it in MinIO, then records it in media_db.
// Form fields: entity_type, entity_id, media_type, file.
// The gateway injects X-User-ID; the caller must own the entity being uploaded to.
func (h *Handler) Upload(c *gin.Context) {
	entityType := c.PostForm("entity_type")
	entityID := c.PostForm("entity_id")
	mediaType := c.PostForm("media_type")

	if !allowedEntityTypes[entityType] {
		errJSON(c, http.StatusBadRequest, "invalid entity_type")
		return
	}
	if !allowedMediaTypes[mediaType] {
		errJSON(c, http.StatusBadRequest, "invalid media_type")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		errJSON(c, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	// Strip params (e.g. "image/jpeg; charset=…")
	contentType = strings.SplitN(contentType, ";", 2)[0]
	ext, ok := allowedMIME[contentType]
	if !ok {
		// Fall back to extension detection from filename
		ext = strings.TrimPrefix(strings.ToLower(filepath.Ext(header.Filename)), ".")
		if _, ok := allowedMIME["image/"+ext]; !ok {
			errJSON(c, http.StatusUnprocessableEntity, "unsupported file type")
			return
		}
		contentType = "image/" + ext
	}

	id := uuid.New().String()
	key := storage.MediaKey(entityType, entityID, mediaType, id, ext)

	if err := h.store.PutObject(c.Request.Context(), key, contentType, header.Size, file); err != nil {
		log.Printf("media upload put: %v", err)
		errJSON(c, http.StatusInternalServerError, "storage upload failed")
		return
	}

	// URL is the gateway-relative path the frontend uses to fetch the file.
	url := "/media/file/" + id

	const q = `
		INSERT INTO media (id, entity_type, entity_id, media_type, url, s3_key, size_bytes, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true)
		RETURNING id, entity_type, entity_id, media_type, url, size_bytes, created_at`

	var rec mediaRecord
	row := h.pool.QueryRow(c.Request.Context(), q, id, entityType, entityID, mediaType, url, key, header.Size)
	if err := row.Scan(&rec.ID, &rec.EntityType, &rec.EntityID, &rec.MediaType, &rec.URL, &rec.SizeBytes, &rec.CreatedAt); err != nil {
		log.Printf("media upload db insert: %v", err)
		// Best-effort cleanup of the orphaned object
		_ = h.store.DeleteObject(c.Request.Context(), key)
		errJSON(c, http.StatusInternalServerError, "failed to record upload")
		return
	}

	c.JSON(http.StatusCreated, rec)
}

// ServeFile streams the media object from MinIO identified by its DB record ID.
func (h *Handler) ServeFile(c *gin.Context) {
	id := c.Param("id")

	var s3Key string
	err := h.pool.QueryRow(c.Request.Context(),
		`SELECT s3_key FROM media WHERE id = $1 AND is_active = true`, id,
	).Scan(&s3Key)
	if err != nil {
		errJSON(c, http.StatusNotFound, "media not found")
		return
	}

	reader, contentType, err := h.store.GetObject(c.Request.Context(), s3Key)
	if err != nil {
		log.Printf("media serve get: %v", err)
		errJSON(c, http.StatusInternalServerError, "could not retrieve file")
		return
	}
	defer reader.Close()

	c.Header("Cache-Control", "public, max-age=3600")
	c.Header("Content-Type", contentType)
	io.Copy(c.Writer, reader)
}

// ListByEntity returns all active media records for an entity.
func (h *Handler) ListByEntity(c *gin.Context) {
	entityType := c.Param("entityType")
	entityID := c.Param("entityID")

	rows, err := h.pool.Query(c.Request.Context(),
		`SELECT id, entity_type, entity_id, media_type, url, width, height, size_bytes, created_at
		 FROM media WHERE entity_type = $1 AND entity_id = $2 AND is_active = true
		 ORDER BY created_at DESC`,
		entityType, entityID,
	)
	if err != nil {
		log.Printf("media list: %v", err)
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	results := []mediaRecord{}
	for rows.Next() {
		var r mediaRecord
		if err := rows.Scan(&r.ID, &r.EntityType, &r.EntityID, &r.MediaType, &r.URL,
			&r.Width, &r.Height, &r.SizeBytes, &r.CreatedAt); err != nil {
			continue
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, gin.H{"media": results})
}

// Delete removes a media record and its backing object from MinIO.
func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")

	var s3Key string
	err := h.pool.QueryRow(c.Request.Context(),
		`UPDATE media SET is_active = false WHERE id = $1 RETURNING s3_key`, id,
	).Scan(&s3Key)
	if err != nil {
		errJSON(c, http.StatusNotFound, "media not found")
		return
	}

	if err := h.store.DeleteObject(c.Request.Context(), s3Key); err != nil {
		log.Printf("media delete object: %v (record already soft-deleted)", err)
	}

	c.Status(http.StatusNoContent)
}
