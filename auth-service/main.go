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

	"dramalist/auth-service/cache"
	"dramalist/auth-service/config"
	"dramalist/auth-service/db"
	"dramalist/auth-service/handler"
	"dramalist/auth-service/keys"
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

	rdb, err := cache.Connect(ctx, cfg)
	if err != nil {
		slog.Error("redis unavailable", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	kp, err := keys.LoadOrGenerate(cfg.KeysDir)
	if err != nil {
		slog.Error("RSA key setup failed", "err", err)
		os.Exit(1)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	h := handler.New(cfg, pool, rdb, kp)
	h.Register(r)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("auth-service listening", "port", cfg.Port)
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
	slog.Info("auth-service stopped")
}
