package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"dramalist/show-service/config"
	"dramalist/show-service/kafka"
)

type Handler struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	producer *kafka.Producer
}

func New(cfg *config.Config, pool *pgxpool.Pool, producer *kafka.Producer) *Handler {
	return &Handler{cfg: cfg, pool: pool, producer: producer}
}

func (h *Handler) Register(r *gin.Engine) {
	r.GET("/health", h.Health)

	// Internal backfill — no gateway location; reachable only within the cluster.
	r.GET("/internal/catalog/all", h.ExportAllCatalog)

	// ── Catalog (public read, admin write) ─────────────────────────────────────
	catalog := r.Group("/catalog")
	catalog.GET("", h.ListCatalog)
	catalog.POST("", h.CreateCatalogEntry)
	catalog.GET("/:id", h.GetCatalogEntry)
	catalog.PATCH("/:id", h.UpdateCatalogEntry)
	catalog.DELETE("/:id", h.DeleteCatalogEntry)

	catalog.GET("/:id/cast", h.GetCast)
	catalog.POST("/:id/cast", h.AddCastMember)
	catalog.PATCH("/:id/cast/:castId", h.UpdateCastMember)
	catalog.DELETE("/:id/cast/:castId", h.RemoveCastMember)

	// ── User list (authenticated) ──────────────────────────────────────────────
	list := r.Group("/list")
	list.GET("/users/:userID", h.ListPublicEntries)
	list.GET("", h.ListEntries)
	list.POST("", h.CreateListEntry)
	list.GET("/:id", h.GetListEntry)
	list.PATCH("/:id", h.UpdateListEntry)
	list.DELETE("/:id", h.DeleteListEntry)

	// ── Actors (public read, admin write) ──────────────────────────────────────
	actors := r.Group("/actors")
	actors.GET("", h.SearchActors)
	actors.POST("", h.CreateActor)
	actors.GET("/:id", h.GetActorProfile)
	actors.PATCH("/:id", h.UpdateActor)

	// ── Discovery (public) ─────────────────────────────────────────────────────
	r.GET("/shows/public/trending", h.TrendingShows)
	r.GET("/shows/public/recent", h.RecentShows)
}

func errJSON(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}
