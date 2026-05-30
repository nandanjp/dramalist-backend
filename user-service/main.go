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
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"dramalist/user-service/cache"
	"dramalist/user-service/config"
	"dramalist/user-service/db"
	"dramalist/user-service/handler"
	"dramalist/user-service/kafka"
	"dramalist/user-service/middleware"
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

	consumer := kafka.NewConsumer(cfg, pool)

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	go consumer.Run(consumerCtx)

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	r := gin.New()
	r.Use(gin.Recovery())
	m := middleware.NewMetrics("user_service")
	r.Use(m.Handler())
	r.Use(middleware.RequestLogger("user_service"))
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	h := handler.New(cfg, pool, rdb)
	h.Register(r)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("user-service listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	consumerCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	slog.Info("user-service stopped")
}
