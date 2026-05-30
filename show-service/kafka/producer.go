package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"dramalist/show-service/config"
)

type CatalogEvent struct {
	Event         string   `json:"event"`
	CatalogID     string   `json:"catalog_id"`
	MediaType     string   `json:"media_type"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title"`
	Genre         []string `json:"genre"`
	AiringStatus  string   `json:"airing_status"`
	Year          *int     `json:"year"`
	Country       *string  `json:"country"`
	Language      *string  `json:"language"`
	Synopsis      *string  `json:"synopsis"`
	PosterURL     *string  `json:"poster_url"`
	IsPublic      bool     `json:"is_public"`
}

type Producer struct {
	writer *kafkago.Writer
}

func NewProducer(cfg *config.Config) *Producer {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	w := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        "catalog.events",
		Balancer:     &kafkago.LeastBytes{},
		RequiredAcks: kafkago.RequireOne,
		WriteTimeout: 5 * time.Second,
	}
	return &Producer{writer: w}
}

func (p *Producer) Close() {
	p.writer.Close()
}

func (p *Producer) Publish(ctx context.Context, evt CatalogEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("kafka marshal failed", "event", evt.Event, "err", err)
		return
	}
	if err := p.writer.WriteMessages(ctx, kafkago.Message{Value: payload}); err != nil {
		slog.Error("kafka publish failed", "event", evt.Event, "err", err)
	}
}
