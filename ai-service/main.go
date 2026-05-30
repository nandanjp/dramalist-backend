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
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"dramalist/ai-service/config"
	"dramalist/ai-service/handler"
	"dramalist/ai-service/llm"
	"dramalist/ai-service/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	client := llm.New(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		err := client.Ping(pingCtx)
		pingCancel()
		if err == nil {
			slog.Info("ollama ready", "url", cfg.OllamaURL, "model", cfg.OllamaModel)
			break
		}
		slog.Warn("ollama not ready, retrying", "err", err)
		time.Sleep(3 * time.Second)
	}

	h := handler.New(client)

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	m := middleware.NewMetrics("ai_service")
	r.Use(m.Handler())
	r.Use(middleware.RequestLogger("ai_service"))
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	h.RegisterRoutes(r)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  180 * time.Second,
		WriteTimeout: 180 * time.Second,
	}

	go func() {
		slog.Info("ai-service starting", "port", cfg.Port)
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
