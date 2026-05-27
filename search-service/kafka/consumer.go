package kafka

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"dramalist/search-service/config"
	"dramalist/search-service/elastic"
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

type Consumer struct {
	reader *kafkago.Reader
	es     *elastic.Client
}

func NewConsumer(cfg *config.Config, es *elastic.Client) *Consumer {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        brokers,
		GroupID:        cfg.KafkaGroupID,
		Topic:          "show.events",
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        time.Second,
		CommitInterval: time.Second,
	})
	return &Consumer{reader: r, es: es}
}

func (c *Consumer) Close() {
	c.reader.Close()
}

func (c *Consumer) Run(ctx context.Context) {
	slog.Info("kafka consumer started", "topic", "show.events")
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("kafka read failed", "err", err)
			continue
		}

		var evt ShowEvent
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			slog.Error("kafka unmarshal failed", "err", err)
			continue
		}

		switch evt.Event {
		case "show.created", "show.updated":
			c.handleUpsert(ctx, evt)
		case "show.deleted":
			c.handleDelete(ctx, evt)
		default:
			slog.Warn("unknown show event", "event", evt.Event)
		}
	}
}

func (c *Consumer) handleUpsert(ctx context.Context, evt ShowEvent) {
	doc := elastic.ShowDoc{
		ShowID:   evt.ShowID,
		UserID:   evt.UserID,
		Title:    evt.Title,
		Genre:    evt.Genre,
		Status:   evt.Status,
		Tags:     evt.Tags,
		Year:     evt.Year,
		IsPublic: evt.IsPublic,
	}
	if evt.OriginalTitle != nil {
		doc.OriginalTitle = *evt.OriginalTitle
	}
	if doc.Genre == nil {
		doc.Genre = []string{}
	}
	if doc.Tags == nil {
		doc.Tags = []string{}
	}

	if err := c.es.IndexShow(ctx, doc); err != nil {
		slog.Error("elasticsearch index failed", "show_id", evt.ShowID, "err", err)
		return
	}
	slog.Info("show indexed", "show_id", evt.ShowID, "event", evt.Event)
}

func (c *Consumer) handleDelete(ctx context.Context, evt ShowEvent) {
	if err := c.es.DeleteShow(ctx, evt.ShowID); err != nil {
		slog.Error("elasticsearch delete failed", "show_id", evt.ShowID, "err", err)
		return
	}
	slog.Info("show deleted from index", "show_id", evt.ShowID)
}
