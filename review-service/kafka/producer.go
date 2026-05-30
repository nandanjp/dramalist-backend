package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"dramalist/review-service/config"
)

// ReviewEvent is published to review.events. user-service consumes review.created
// to update watch_stats; show_genres and show_episode_count must be supplied by
// the caller so this service stays decoupled from show-service.
type ReviewEvent struct {
	Event            string   `json:"event"`
	ReviewID         string   `json:"review_id"`
	UserID           string   `json:"user_id"`
	CatalogID        string   `json:"catalog_id"`
	Rating           float64  `json:"rating"`
	OldRating        *float64 `json:"old_rating,omitempty"` // set on review.updated so user-service can adjust running avg
	ShowGenres       []string `json:"genres"`
	ShowEpisodeCount int      `json:"episode_count"`
}

type Producer struct {
	writer *kafkago.Writer
}

func NewProducer(cfg *config.Config) *Producer {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	w := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        "review.events",
		Balancer:     &kafkago.LeastBytes{},
		RequiredAcks: kafkago.RequireOne,
		WriteTimeout: 5 * time.Second,
	}
	return &Producer{writer: w}
}

func (p *Producer) Close() {
	p.writer.Close()
}

func (p *Producer) Publish(ctx context.Context, evt ReviewEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("kafka marshal failed", "event", evt.Event, "err", err)
		return
	}
	if err := p.writer.WriteMessages(ctx, kafkago.Message{Value: payload}); err != nil {
		slog.Error("kafka publish failed", "event", evt.Event, "err", err)
	}
}
