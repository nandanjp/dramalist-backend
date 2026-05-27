package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"

	"dramalist/user-service/config"
)

// ReviewEvent is the shape published by review-service on review.events.
type ReviewEvent struct {
	Event        string   `json:"event"`
	ReviewID     string   `json:"review_id"`
	UserID       string   `json:"user_id"`
	Rating       float64  `json:"rating"`
	OldRating    *float64 `json:"old_rating"`   // non-nil on review.updated
	Genres       []string `json:"genres"`        // populated on review.created only
	EpisodeCount int      `json:"episode_count"` // populated on review.created only
}

type Consumer struct {
	reader *kafkago.Reader
	pool   *pgxpool.Pool
}

func NewConsumer(cfg *config.Config, pool *pgxpool.Pool) *Consumer {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     brokers,
		GroupID:     cfg.KafkaGroupID,
		Topic:       "review.events",
		MinBytes:    10e3,
		MaxBytes:    10e6,
		MaxWait:     1 * time.Second,
		StartOffset: kafkago.FirstOffset,
	})
	return &Consumer{reader: r, pool: pool}
}

func (c *Consumer) Run(ctx context.Context) {
	slog.Info("kafka consumer started", "topic", "review.events")
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Error("kafka read error", "err", err)
			continue
		}

		var evt ReviewEvent
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			slog.Warn("kafka message decode error", "err", err)
			continue
		}

		var handlerErr error
		switch evt.Event {
		case "review.created":
			handlerErr = c.handleCreated(ctx, evt)
		case "review.updated":
			handlerErr = c.handleUpdated(ctx, evt)
		case "review.deleted":
			handlerErr = c.handleDeleted(ctx, evt)
		default:
			continue
		}

		if handlerErr != nil {
			slog.Error("watch_stats update failed", "event", evt.Event, "user_id", evt.UserID, "err", handlerErr)
		}
	}
	c.reader.Close()
	slog.Info("kafka consumer stopped")
}

// ── shared helpers ────────────────────────────────────────────────────────────

type watchStats struct {
	totalWatched   int
	totalEpisodes  int
	avgRating      *float64
	genreBreakdown map[string]int
}

func (c *Consumer) fetchStats(ctx context.Context, userID string) (watchStats, error) {
	var ws watchStats
	ws.genreBreakdown = make(map[string]int)

	var genreBytes []byte
	err := c.pool.QueryRow(ctx,
		"SELECT total_watched, total_episodes, avg_rating, genre_breakdown FROM watch_stats WHERE user_id = $1",
		userID,
	).Scan(&ws.totalWatched, &ws.totalEpisodes, &ws.avgRating, &genreBytes)
	if err != nil {
		return ws, fmt.Errorf("fetch watch_stats: %w", err)
	}
	json.Unmarshal(genreBytes, &ws.genreBreakdown) //nolint:errcheck — safe default
	return ws, nil
}

func (c *Consumer) ensureRow(ctx context.Context, userID string) error {
	_, err := c.pool.Exec(ctx,
		"INSERT INTO watch_stats (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING",
		userID,
	)
	return err
}

func roundRating(v float64) float64 {
	return math.Round(v*10) / 10
}

func (c *Consumer) writeStats(ctx context.Context, userID string, ws watchStats) error {
	genreJSON, _ := json.Marshal(ws.genreBreakdown)
	_, err := c.pool.Exec(ctx,
		`UPDATE watch_stats
		 SET total_watched   = $2,
		     total_episodes  = $3,
		     avg_rating      = $4,
		     genre_breakdown = $5,
		     updated_at      = NOW()
		 WHERE user_id = $1`,
		userID, ws.totalWatched, ws.totalEpisodes, ws.avgRating, genreJSON,
	)
	return err
}

// ── review.created ────────────────────────────────────────────────────────────

func (c *Consumer) handleCreated(ctx context.Context, evt ReviewEvent) error {
	if err := c.ensureRow(ctx, evt.UserID); err != nil {
		return fmt.Errorf("ensure row: %w", err)
	}

	ws, err := c.fetchStats(ctx, evt.UserID)
	if err != nil {
		return err
	}

	newTotal := ws.totalWatched + 1
	newEpisodes := ws.totalEpisodes + evt.EpisodeCount

	var newAvg *float64
	currentSum := 0.0
	if ws.avgRating != nil {
		currentSum = *ws.avgRating * float64(ws.totalWatched)
	}
	avg := roundRating((currentSum + evt.Rating) / float64(newTotal))
	newAvg = &avg

	for _, g := range evt.Genres {
		if g != "" {
			ws.genreBreakdown[g]++
		}
	}

	ws.totalWatched = newTotal
	ws.totalEpisodes = newEpisodes
	ws.avgRating = newAvg
	return c.writeStats(ctx, evt.UserID, ws)
}

// ── review.updated ────────────────────────────────────────────────────────────

func (c *Consumer) handleUpdated(ctx context.Context, evt ReviewEvent) error {
	if evt.OldRating == nil {
		// Malformed event — skip rather than corrupt stats.
		slog.Warn("review.updated missing old_rating, skipping", "review_id", evt.ReviewID)
		return nil
	}

	if err := c.ensureRow(ctx, evt.UserID); err != nil {
		return fmt.Errorf("ensure row: %w", err)
	}

	ws, err := c.fetchStats(ctx, evt.UserID)
	if err != nil {
		return err
	}

	if ws.totalWatched > 0 && ws.avgRating != nil {
		// Adjust running average: new_avg = (old_avg×count − old_rating + new_rating) / count
		adjusted := (*ws.avgRating*float64(ws.totalWatched) - *evt.OldRating + evt.Rating) / float64(ws.totalWatched)
		avg := roundRating(adjusted)
		ws.avgRating = &avg
	}

	return c.writeStats(ctx, evt.UserID, ws)
}

// ── review.deleted ────────────────────────────────────────────────────────────

func (c *Consumer) handleDeleted(ctx context.Context, evt ReviewEvent) error {
	if err := c.ensureRow(ctx, evt.UserID); err != nil {
		return fmt.Errorf("ensure row: %w", err)
	}

	ws, err := c.fetchStats(ctx, evt.UserID)
	if err != nil {
		return err
	}

	if ws.totalWatched <= 1 {
		ws.totalWatched = 0
		ws.avgRating = nil
	} else {
		// Remove this rating from the running average.
		adjusted := (*ws.avgRating*float64(ws.totalWatched) - evt.Rating) / float64(ws.totalWatched-1)
		avg := roundRating(adjusted)
		ws.avgRating = &avg
		ws.totalWatched--
	}

	return c.writeStats(ctx, evt.UserID, ws)
}
