package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	kafkago "github.com/segmentio/kafka-go"

	"dramalist/review-service/config"
)

type ShowEvent struct {
	Event  string `json:"event"`
	ShowID string `json:"show_id"`
}

type ShowConsumer struct {
	reader *kafkago.Reader
	pool   *pgxpool.Pool
}

func NewShowConsumer(cfg *config.Config, pool *pgxpool.Pool) *ShowConsumer {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     brokers,
		GroupID:     cfg.KafkaGroupID + "-shows",
		Topic:       "show.events",
		MinBytes:    10e3,
		MaxBytes:    10e6,
		MaxWait:     1 * time.Second,
		StartOffset: kafkago.FirstOffset,
	})
	return &ShowConsumer{reader: r, pool: pool}
}

func (c *ShowConsumer) Close() {
	c.reader.Close()
}

func (c *ShowConsumer) Run(ctx context.Context) {
	slog.Info("kafka consumer started", "topic", "show.events")
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Error("show consumer read error", "err", err)
			continue
		}

		var evt ShowEvent
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			slog.Warn("show event decode error", "err", err)
			continue
		}

		if evt.Event != "show.deleted" {
			continue
		}

		tag, err := c.pool.Exec(ctx,
			"DELETE FROM reviews WHERE show_id = $1", evt.ShowID,
		)
		if err != nil {
			slog.Error("orphan review cleanup failed", "show_id", evt.ShowID, "err", err)
			continue
		}
		if tag.RowsAffected() > 0 {
			slog.Info("orphan reviews deleted", "show_id", evt.ShowID, "count", tag.RowsAffected())
		}
	}
	c.reader.Close()
	slog.Info("show consumer stopped")
}
