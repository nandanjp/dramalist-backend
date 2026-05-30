package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"dramalist/search-service/config"
	"dramalist/search-service/elastic"
	"dramalist/search-service/handler"
	kafkaconsumer "dramalist/search-service/kafka"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	es, err := elastic.New(cfg)
	if err != nil {
		slog.Error("elasticsearch client failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		if err := es.EnsureIndex(ctx); err != nil {
			slog.Warn("elasticsearch not ready, retrying", "err", err)
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}

	// If the index is empty (cold start or wiped volume), backfill from show-service.
	count, err := es.CountDocuments(ctx)
	if err != nil {
		slog.Warn("could not count ES documents, skipping backfill", "err", err)
	} else if count == 0 {
		go backfill(ctx, es, cfg.ShowServiceURL)
	}

	consumer := kafkaconsumer.NewConsumer(cfg, es)
	defer consumer.Close()
	go consumer.Run(ctx)

	h := handler.New(es)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	h.RegisterRoutes(r)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		slog.Info("search-service starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
}

// backfill fetches all catalog entries from show-service and indexes them into ES.
// Runs in a background goroutine so the HTTP server starts immediately.
func backfill(ctx context.Context, es *elastic.Client, showServiceURL string) {
	slog.Info("starting ES backfill from show-service")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, showServiceURL+"/internal/catalog/all", nil)
	if err != nil {
		slog.Error("backfill: build request failed", "err", err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("backfill: request to show-service failed", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("backfill: show-service returned non-200", "status", resp.StatusCode)
		return
	}

	var entries []elastic.CatalogDoc
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		slog.Error("backfill: decode failed", "err", err)
		return
	}

	indexed := 0
	for _, doc := range entries {
		if ctx.Err() != nil {
			break
		}
		if err := es.IndexCatalog(ctx, doc); err != nil {
			slog.Error("backfill: index failed", "catalog_id", doc.CatalogID, "err", err)
			continue
		}
		indexed++
	}
	slog.Info("ES backfill complete", "indexed", indexed, "total", len(entries))
}
