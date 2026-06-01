package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"
)

var imageHTTPClient = &http.Client{Timeout: 20 * time.Second}

// mirrorImage downloads srcURL and uploads it to the media-service, returning
// the proxy URL (e.g. "/media/file/{id}?size=medium") or empty string on failure.
// Failures are logged as warnings but never propagate to callers.
func (h *Handler) mirrorImage(ctx context.Context, srcURL, entityType, entityID, mediaType, userID string) string {
	if srcURL == "" {
		return ""
	}

	// Download source image
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		slog.Warn("mirrorImage: build request failed", "url", srcURL, "err", err)
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; dramalist-importer/1.0)")
	resp, err := imageHTTPClient.Do(req)
	if err != nil {
		slog.Warn("mirrorImage: download failed", "url", srcURL, "err", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("mirrorImage: download non-200", "url", srcURL, "status", resp.StatusCode)
		return ""
	}
	rawBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("mirrorImage: read failed", "url", srcURL, "err", err)
		return ""
	}

	// Detect content-type: prefer header, fall back to extension
	ct := strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]
	ct = strings.TrimSpace(ct)
	if ct == "" || ct == "application/octet-stream" {
		ext := strings.ToLower(path.Ext(strings.SplitN(srcURL, "?", 2)[0]))
		switch ext {
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".png":
			ct = "image/png"
		case ".webp":
			ct = "image/webp"
		default:
			ct = "image/jpeg"
		}
	}
	if ct != "image/jpeg" && ct != "image/png" && ct != "image/gif" && ct != "image/webp" {
		slog.Warn("mirrorImage: unsupported content-type", "url", srcURL, "ct", ct)
		return ""
	}

	// Build multipart upload
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("entity_type", entityType)
	_ = w.WriteField("entity_id", entityID)
	_ = w.WriteField("media_type", mediaType)
	fw, err := w.CreateFormFile("file", "image")
	if err != nil {
		slog.Warn("mirrorImage: create form file failed", "err", err)
		return ""
	}
	if _, err := fw.Write(rawBytes); err != nil {
		slog.Warn("mirrorImage: write form file failed", "err", err)
		return ""
	}
	w.Close()

	uploadURL := h.mediaURL + "/media/upload"
	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
	if err != nil {
		slog.Warn("mirrorImage: build upload request failed", "err", err)
		return ""
	}
	uploadReq.Header.Set("Content-Type", w.FormDataContentType())
	uploadReq.Header.Set("X-User-Id", userID)

	uploadResp, err := imageHTTPClient.Do(uploadReq)
	if err != nil {
		slog.Warn("mirrorImage: upload failed", "url", uploadURL, "err", err)
		return ""
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		body2, _ := io.ReadAll(uploadResp.Body)
		slog.Warn("mirrorImage: upload non-201", "status", uploadResp.StatusCode, "body", string(body2))
		return ""
	}

	var result struct {
		MediumURL string `json:"medium_url"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&result); err != nil {
		slog.Warn("mirrorImage: decode response failed", "err", err)
		return ""
	}
	if result.MediumURL == "" {
		return ""
	}
	slog.Info("mirrorImage: success", "entity_type", entityType, "entity_id", entityID, "url", result.MediumURL)
	return fmt.Sprintf("/api%s", result.MediumURL)
}
