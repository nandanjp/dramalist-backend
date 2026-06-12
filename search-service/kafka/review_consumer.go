package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"dramalist/search-service/config"
	"dramalist/search-service/meili"
)

type ReviewEvent struct {
	Event     string  `json:"event"`
	ReviewID  string  `json:"review_id"`
	UserID    string  `json:"user_id"`
	CatalogID string  `json:"catalog_id"`
	Rating    float64 `json:"rating"`
}

type ReviewConsumer struct {
	reader    *kafkago.Reader
	es        *meili.Client
	reviewURL string
	httpClient *http.Client
}

func NewReviewConsumer(cfg *config.Config, es *meili.Client) *ReviewConsumer {
	brokers := strings.Split(cfg.KafkaBootstrapServers, ",")
	r := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        brokers,
		GroupID:        cfg.KafkaGroupID + "-review",
		Topic:          "review.events",
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        time.Second,
		CommitInterval: time.Second,
	})
	return &ReviewConsumer{
		reader:    r,
		es:        es,
		reviewURL: cfg.ReviewServiceURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *ReviewConsumer) Close() {
	c.reader.Close()
}

func (c *ReviewConsumer) Run(ctx context.Context) {
	slog.Info("kafka consumer started", "topic", "review.events")
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("review kafka read failed", "err", err)
			continue
		}

		var evt ReviewEvent
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			slog.Error("review kafka unmarshal failed", "err", err)
			continue
		}

		if evt.CatalogID == "" {
			continue
		}

		switch evt.Event {
		case "review.created", "review.updated", "review.deleted":
			c.updateRating(ctx, evt.CatalogID)
		}
	}
}

// updateRating fetches the current aggregate from review-service and patches the Meilisearch doc.
func (c *ReviewConsumer) updateRating(ctx context.Context, catalogID string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/reviews/aggregate/%s", c.reviewURL, catalogID), nil)
	if err != nil {
		slog.Error("review update: build request failed", "catalog_id", catalogID, "err", err)
		return
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("review update: request failed", "catalog_id", catalogID, "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("review update: aggregate endpoint returned non-200", "catalog_id", catalogID, "status", resp.StatusCode)
		return
	}

	var agg struct {
		AvgRating   *float64 `json:"avg_rating"`
		ReviewCount int      `json:"review_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&agg); err != nil {
		slog.Error("review update: decode failed", "catalog_id", catalogID, "err", err)
		return
	}

	avgRating := 0.0
	if agg.AvgRating != nil {
		avgRating = *agg.AvgRating
	}

	if err := c.es.UpdateRating(ctx, catalogID, avgRating, agg.ReviewCount); err != nil {
		slog.Error("review update: meilisearch update failed", "catalog_id", catalogID, "err", err)
		return
	}
	slog.Info("review rating updated", "catalog_id", catalogID, "avg_rating", avgRating)
}
