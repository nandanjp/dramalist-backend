package handler

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"dramalist/media-service/storage"
)

var allowedEntityTypes = map[string]bool{"catalog": true, "actor": true, "user": true}
var allowedMediaTypes = map[string]bool{"poster": true, "banner": true, "thumbnail": true, "avatar": true, "profile": true}

// allowedInputMIME maps accepted upload MIME types to a canonical label.
// We accept any common image format — it will be converted to WebP regardless.
var allowedInputMIME = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

type mediaRecord struct {
	ID         string `json:"id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	MediaType  string `json:"media_type"`
	ThumbURL   string `json:"thumb_url"`
	MediumURL  string `json:"medium_url"`
	LargeURL   string `json:"large_url"`
	SizeBytes  *int64 `json:"size_bytes,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// Upload accepts a multipart image and stores three WebP variants (thumb/medium/large)
// in MinIO, then records them in media_db as a single row.
// Form fields: entity_type, entity_id, media_type, file.
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

	// Validate MIME type — strip any "; param=value" suffix before lookup.
	ct := strings.TrimSpace(strings.SplitN(header.Header.Get("Content-Type"), ";", 2)[0])
	if !allowedInputMIME[ct] {
		errJSON(c, http.StatusUnprocessableEntity, "unsupported file type (jpeg, png, gif, webp accepted)")
		return
	}

	// Read entire file into memory so we can both process and measure it.
	raw, err := io.ReadAll(file)
	if err != nil {
		errJSON(c, http.StatusBadRequest, "could not read file")
		return
	}
	originalSize := int64(len(raw))

	variants, err := storage.ProcessImage(bytes.NewReader(raw))
	if err != nil {
		log.Printf("media process image: %v", err)
		errJSON(c, http.StatusUnprocessableEntity, "could not process image")
		return
	}

	id := uuid.New().String()
	ctx := c.Request.Context()

	keyThumb := storage.MediaKeyVariant(entityType, entityID, mediaType, id, "thumb")
	keyMedium := storage.MediaKeyVariant(entityType, entityID, mediaType, id, "medium")
	keyLarge := storage.MediaKeyVariant(entityType, entityID, mediaType, id, "large")

	putVariant := func(key string, data []byte) error {
		return h.store.PutObject(ctx, key, "image/webp", int64(len(data)), bytes.NewReader(data))
	}

	if err := putVariant(keyThumb, variants.Thumb); err != nil {
		log.Printf("media upload thumb: %v", err)
		errJSON(c, http.StatusInternalServerError, "storage upload failed")
		return
	}
	if err := putVariant(keyMedium, variants.Medium); err != nil {
		log.Printf("media upload medium: %v", err)
		_ = h.store.DeleteObject(ctx, keyThumb)
		errJSON(c, http.StatusInternalServerError, "storage upload failed")
		return
	}
	if err := putVariant(keyLarge, variants.Large); err != nil {
		log.Printf("media upload large: %v", err)
		_ = h.store.DeleteObject(ctx, keyThumb)
		_ = h.store.DeleteObject(ctx, keyMedium)
		errJSON(c, http.StatusInternalServerError, "storage upload failed")
		return
	}

	thumbURL := "/media/file/" + id + "?size=thumb"
	mediumURL := "/media/file/" + id + "?size=medium"
	largeURL := "/media/file/" + id + "?size=large"

	const q = `
		INSERT INTO media
		    (id, entity_type, entity_id, media_type, thumb_url, medium_url, large_url,
		     s3_key_prefix, size_bytes, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, true)
		RETURNING id, entity_type, entity_id, media_type, thumb_url, medium_url, large_url,
		          size_bytes, created_at`

	keyPrefix := storage.MediaKeyPrefix(entityType, entityID, mediaType, id)

	var rec mediaRecord
	row := h.pool.QueryRow(ctx, q,
		id, entityType, entityID, mediaType,
		thumbURL, mediumURL, largeURL,
		keyPrefix, originalSize,
	)
	if err := row.Scan(
		&rec.ID, &rec.EntityType, &rec.EntityID, &rec.MediaType,
		&rec.ThumbURL, &rec.MediumURL, &rec.LargeURL,
		&rec.SizeBytes, &rec.CreatedAt,
	); err != nil {
		log.Printf("media upload db insert: %v", err)
		_ = h.store.DeleteObject(ctx, keyThumb)
		_ = h.store.DeleteObject(ctx, keyMedium)
		_ = h.store.DeleteObject(ctx, keyLarge)
		errJSON(c, http.StatusInternalServerError, "failed to record upload")
		return
	}

	c.JSON(http.StatusCreated, rec)
}

// ServeFile streams a WebP variant from MinIO identified by its DB record ID.
// Query param ?size=thumb|medium|large selects the variant; defaults to medium.
func (h *Handler) ServeFile(c *gin.Context) {
	id := c.Param("id")
	size := c.DefaultQuery("size", "medium")
	if size != "thumb" && size != "medium" && size != "large" {
		size = "medium"
	}

	var prefix string
	err := h.pool.QueryRow(c.Request.Context(),
		`SELECT s3_key_prefix FROM media WHERE id = $1 AND is_active = true`, id,
	).Scan(&prefix)
	if err != nil {
		errJSON(c, http.StatusNotFound, "media not found")
		return
	}

	// s3_key_prefix is stored as the key pattern with a trailing underscore-less base;
	// we stored it via MediaKeyVariant(..., "") which gives "{...}/{id}_" — derive variant key.
	key := prefix + size + ".webp"

	reader, _, err := h.store.GetObject(c.Request.Context(), key)
	if err != nil {
		log.Printf("media serve get: %v", err)
		errJSON(c, http.StatusInternalServerError, "could not retrieve file")
		return
	}
	defer reader.Close()

	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Type", "image/webp")
	io.Copy(c.Writer, reader)
}

// ListByEntity returns all active media records for an entity.
func (h *Handler) ListByEntity(c *gin.Context) {
	entityType := c.Param("entityType")
	entityID := c.Param("entityID")

	rows, err := h.pool.Query(c.Request.Context(),
		`SELECT id, entity_type, entity_id, media_type, thumb_url, medium_url, large_url,
		        size_bytes, created_at
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
		if err := rows.Scan(
			&r.ID, &r.EntityType, &r.EntityID, &r.MediaType,
			&r.ThumbURL, &r.MediumURL, &r.LargeURL,
			&r.SizeBytes, &r.CreatedAt,
		); err != nil {
			continue
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, gin.H{"media": results})
}

// Delete soft-deletes a media record and removes all three variant objects from MinIO.
func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")

	var prefix string
	err := h.pool.QueryRow(c.Request.Context(),
		`UPDATE media SET is_active = false WHERE id = $1 RETURNING s3_key_prefix`, id,
	).Scan(&prefix)
	if err != nil {
		errJSON(c, http.StatusNotFound, "media not found")
		return
	}

	ctx := c.Request.Context()
	for _, size := range []string{"thumb", "medium", "large"} {
		if err := h.store.DeleteObject(ctx, prefix+size+".webp"); err != nil {
			log.Printf("media delete object %s%s.webp: %v (record already soft-deleted)", prefix, size, err)
		}
	}

	c.Status(http.StatusNoContent)
}
