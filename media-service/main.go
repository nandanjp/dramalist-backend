package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"dramalist/media-service/config"
	"dramalist/media-service/db"
	"dramalist/media-service/handler"
	"dramalist/media-service/storage"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, cfg)
	if err != nil {
		slog.Error("postgres unavailable", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	store, err := storage.Connect(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey)
	if err != nil {
		slog.Error("minio unavailable", "err", err)
		os.Exit(1)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	h := handler.New(cfg, pool, store)
	h.Register(r)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("media-service listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	slog.Info("media-service stopped")
}
