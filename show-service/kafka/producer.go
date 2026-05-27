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

type ShowEvent struct {
	Event         string   `json:"event"`
	ShowID        string   `json:"show_id"`
	UserID        string   `json:"user_id"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title"`
	Genre         []string `json:"genre"`
	Status        string   `json:"status"`
	Tags          []string `json:"tags"`
	Year          *int     `json:"year"`
	IsPublic      bool     `json:"is_public"`
}

type Producer struct {
	writer *kafkago.Writer
}

func NewProducer(cfg *config.Config) *Producer {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	w := &kafkago.Writer{
		Addr:         kafkago.TCP(brokers...),
		Topic:        "show.events",
		Balancer:     &kafkago.LeastBytes{},
		RequiredAcks: kafkago.RequireOne,
		WriteTimeout: 5 * time.Second,
	}
	return &Producer{writer: w}
}

func (p *Producer) Close() {
	p.writer.Close()
}

func (p *Producer) Publish(ctx context.Context, evt ShowEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("kafka marshal failed", "event", evt.Event, "err", err)
		return
	}
	if err := p.writer.WriteMessages(ctx, kafkago.Message{Value: payload}); err != nil {
		slog.Error("kafka publish failed", "event", evt.Event, "err", err)
	}
}
