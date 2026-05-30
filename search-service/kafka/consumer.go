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

type CatalogEvent struct {
	Event         string   `json:"event"`
	CatalogID     string   `json:"catalog_id"`
	MediaType     string   `json:"media_type"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title"`
	Synopsis      *string  `json:"synopsis"`
	Genre         []string `json:"genre"`
	AiringStatus  string   `json:"airing_status"`
	Year          *int     `json:"year"`
	Country       *string  `json:"country"`
	Language      *string  `json:"language"`
	PosterURL     *string  `json:"poster_url"`
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
		Topic:          "catalog.events",
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
	slog.Info("kafka consumer started", "topic", "catalog.events")
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("kafka read failed", "err", err)
			continue
		}

		var evt CatalogEvent
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			slog.Error("kafka unmarshal failed", "err", err)
			continue
		}

		switch evt.Event {
		case "catalog.created", "catalog.updated":
			c.handleUpsert(ctx, evt)
		case "catalog.deleted":
			c.handleDelete(ctx, evt)
		default:
			slog.Warn("unknown catalog event", "event", evt.Event)
		}
	}
}

func (c *Consumer) handleUpsert(ctx context.Context, evt CatalogEvent) {
	doc := elastic.CatalogDoc{
		CatalogID:    evt.CatalogID,
		MediaType:    evt.MediaType,
		Title:        evt.Title,
		AiringStatus: evt.AiringStatus,
		Year:         evt.Year,
		Genre:        evt.Genre,
		ActorNames:   []string{},
	}
	if evt.OriginalTitle != nil {
		doc.OriginalTitle = *evt.OriginalTitle
	}
	if evt.Synopsis != nil {
		doc.Synopsis = *evt.Synopsis
	}
	if evt.Country != nil {
		doc.Country = *evt.Country
	}
	if evt.Language != nil {
		doc.Language = *evt.Language
	}
	if evt.PosterURL != nil {
		doc.PosterURL = *evt.PosterURL
	}
	if doc.Genre == nil {
		doc.Genre = []string{}
	}

	if err := c.es.IndexCatalog(ctx, doc); err != nil {
		slog.Error("elasticsearch index failed", "catalog_id", evt.CatalogID, "err", err)
		return
	}
	slog.Info("catalog indexed", "catalog_id", evt.CatalogID, "event", evt.Event)
}

func (c *Consumer) handleDelete(ctx context.Context, evt CatalogEvent) {
	if err := c.es.DeleteCatalog(ctx, evt.CatalogID); err != nil {
		slog.Error("elasticsearch delete failed", "catalog_id", evt.CatalogID, "err", err)
		return
	}
	slog.Info("catalog deleted from index", "catalog_id", evt.CatalogID)
}
