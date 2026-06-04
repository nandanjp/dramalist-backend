package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"dramalist/drama-service/cache"
	"dramalist/drama-service/config"
	"dramalist/drama-service/db"
	"dramalist/drama-service/handler"
	"dramalist/drama-service/mdl"
	"dramalist/drama-service/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Connect(ctx, cfg)
	if err != nil {
		slog.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb, err := cache.Connect(ctx, cfg)
	if err != nil {
		slog.Warn("redis unavailable, cache invalidation disabled", "err", err)
		rdb = nil
	} else {
		defer rdb.Close()
	}

	client := mdl.NewClient()
	h := handler.New(pool, client, cfg.MediaServiceURL, rdb)

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	m := middleware.NewMetrics("drama_service")
	r.Use(m.Handler())
	r.Use(middleware.RequestLogger("drama_service"))
	h.RegisterRoutes(r)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		slog.Info("drama-service starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx) //nolint:errcheck
}
