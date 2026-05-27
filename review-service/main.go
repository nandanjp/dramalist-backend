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

	"dramalist/review-service/cache"
	"dramalist/review-service/config"
	"dramalist/review-service/db"
	"dramalist/review-service/handler"
	"dramalist/review-service/kafka"
)

func main() {
	cfg := config.Load()

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer connectCancel()

	pool, err := db.Connect(connectCtx, cfg)
	if err != nil {
		slog.Error("postgres unavailable", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb, err := cache.Connect(connectCtx, cfg)
	if err != nil {
		slog.Error("redis unavailable", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	producer := kafka.NewProducer(cfg)
	defer producer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	showConsumer := kafka.NewShowConsumer(cfg, pool)
	defer showConsumer.Close()
	go showConsumer.Run(ctx)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	h := handler.New(cfg, pool, rdb, producer)
	h.Register(r)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("review-service listening", "port", cfg.Port)
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
	slog.Info("review-service stopped")
}
