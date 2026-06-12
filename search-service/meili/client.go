package meili

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	meilisearch "github.com/meilisearch/meilisearch-go"

	"dramalist/search-service/config"
)

const indexUID = "catalog"

type CatalogDoc struct {
	CatalogID     string   `json:"catalog_id"`
	MediaType     string   `json:"media_type"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title,omitempty"`
	Synopsis      string   `json:"synopsis,omitempty"`
	Genre         []string `json:"genre"`
	AiringStatus  string   `json:"airing_status"`
	Year          *int     `json:"year,omitempty"`
	Country       string   `json:"country,omitempty"`
	Language      string   `json:"language,omitempty"`
	PosterURL     string   `json:"poster_url,omitempty"`
	ActorNames    []string `json:"actor_names"`
	AvgRating     float64  `json:"avg_rating,omitempty"`
	ReviewCount   int      `json:"review_count,omitempty"`
}

type SearchResult struct {
	CatalogID     string   `json:"catalog_id"`
	MediaType     string   `json:"media_type"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title,omitempty"`
	Synopsis      string   `json:"synopsis,omitempty"`
	Genre         []string `json:"genre"`
	AiringStatus  string   `json:"airing_status"`
	Year          *int     `json:"year,omitempty"`
	Country       string   `json:"country,omitempty"`
	Language      string   `json:"language,omitempty"`
	PosterURL     string   `json:"poster_url,omitempty"`
	AvgRating     float64  `json:"avg_rating,omitempty"`
	ReviewCount   int      `json:"review_count,omitempty"`
}

type SearchParams struct {
	Query        string
	MediaType    string
	Genre        string
	YearFrom     int
	YearTo       int
	Country      string
	Language     string
	AiringStatus string
	Page         int
	Limit        int
}

type Client struct {
	meili meilisearch.ServiceManager
	index meilisearch.IndexManager
}

func New(cfg *config.Config) (*Client, error) {
	opts := []meilisearch.Option{}
	if cfg.MeilisearchAPIKey != "" {
		opts = append(opts, meilisearch.WithAPIKey(cfg.MeilisearchAPIKey))
	}
	c := meilisearch.New(cfg.MeilisearchURL, opts...)
	return &Client{
		meili: c,
		index: c.Index(indexUID),
	}, nil
}

// EnsureIndex creates the catalog index (if absent) and configures
// searchable/filterable attributes. Safe to call on every startup.
func (c *Client) EnsureIndex(ctx context.Context) error {
	if _, err := c.meili.GetIndex(indexUID); err != nil {
		task, cerr := c.meili.CreateIndex(&meilisearch.IndexConfig{
			Uid:        indexUID,
			PrimaryKey: "catalog_id",
		})
		if cerr != nil {
			return fmt.Errorf("create meili index: %w", cerr)
		}
		if _, cerr = c.meili.WaitForTask(task.TaskUID, 500*time.Millisecond); cerr != nil {
			return fmt.Errorf("wait index creation: %w", cerr)
		}
		slog.Info("meilisearch index created", "index", indexUID)
	}

	task, err := c.index.UpdateSettings(&meilisearch.Settings{
		SearchableAttributes: []string{
			"title", "original_title", "synopsis", "actor_names",
		},
		FilterableAttributes: []string{
			"media_type", "genre", "country", "language", "airing_status", "year",
		},
		SortableAttributes: []string{"year", "avg_rating"},
	})
	if err != nil {
		return fmt.Errorf("update meili settings: %w", err)
	}
	if _, err = c.meili.WaitForTask(task.TaskUID, 500*time.Millisecond); err != nil {
		return fmt.Errorf("wait settings update: %w", err)
	}
	return nil
}

func (c *Client) CountDocuments(ctx context.Context) (int64, error) {
	stats, err := c.index.GetStats(nil)
	if err != nil {
		return 0, fmt.Errorf("meili stats: %w", err)
	}
	return stats.NumberOfDocuments, nil
}

func (c *Client) IndexCatalog(ctx context.Context, doc CatalogDoc) error {
	pk := "catalog_id"
	if _, err := c.index.AddDocuments([]CatalogDoc{doc}, &meilisearch.DocumentOptions{PrimaryKey: &pk}); err != nil {
		return fmt.Errorf("meili add document: %w", err)
	}
	return nil
}

// UpdateRating performs a partial update on an existing catalog document,
// setting only avg_rating and review_count. Uses the same primary key (catalog_id).
func (c *Client) UpdateRating(ctx context.Context, catalogID string, avgRating float64, reviewCount int) error {
	pk := "catalog_id"
	doc := map[string]any{
		"catalog_id":   catalogID,
		"avg_rating":   avgRating,
		"review_count": reviewCount,
	}
	if _, err := c.index.UpdateDocuments([]map[string]any{doc}, &meilisearch.DocumentOptions{PrimaryKey: &pk}); err != nil {
		return fmt.Errorf("meili update rating: %w", err)
	}
	return nil
}

func (c *Client) DeleteCatalog(ctx context.Context, catalogID string) error {
	if _, err := c.index.DeleteDocument(catalogID, nil); err != nil {
		return fmt.Errorf("meili delete document: %w", err)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, p SearchParams) ([]SearchResult, int64, error) {
	filters := make([]string, 0, 6)
	if p.MediaType != "" {
		filters = append(filters, fmt.Sprintf(`media_type = "%s"`, p.MediaType))
	}
	if p.Genre != "" {
		filters = append(filters, fmt.Sprintf(`genre = "%s"`, p.Genre))
	}
	if p.Country != "" {
		filters = append(filters, fmt.Sprintf(`country = "%s"`, p.Country))
	}
	if p.Language != "" {
		filters = append(filters, fmt.Sprintf(`language = "%s"`, p.Language))
	}
	if p.AiringStatus != "" {
		filters = append(filters, fmt.Sprintf(`airing_status = "%s"`, p.AiringStatus))
	}
	if p.YearFrom > 0 {
		filters = append(filters, fmt.Sprintf(`year >= %d`, p.YearFrom))
	}
	if p.YearTo > 0 {
		filters = append(filters, fmt.Sprintf(`year <= %d`, p.YearTo))
	}

	req := &meilisearch.SearchRequest{
		Limit:  int64(p.Limit),
		Offset: int64((p.Page - 1) * p.Limit),
	}
	if len(filters) > 0 {
		req.Filter = strings.Join(filters, " AND ")
	}

	raw, err := c.index.Search(p.Query, req)
	if err != nil {
		return nil, 0, fmt.Errorf("meili search: %w", err)
	}

	// Hits come back as []interface{}; round-trip through JSON for clean unmarshaling.
	hitsJSON, err := json.Marshal(raw.Hits)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal hits: %w", err)
	}
	var docs []CatalogDoc
	if err := json.Unmarshal(hitsJSON, &docs); err != nil {
		return nil, 0, fmt.Errorf("unmarshal hits: %w", err)
	}

	results := make([]SearchResult, len(docs))
	for i, doc := range docs {
		results[i] = SearchResult{
			CatalogID:     doc.CatalogID,
			MediaType:     doc.MediaType,
			Title:         doc.Title,
			OriginalTitle: doc.OriginalTitle,
			Synopsis:      doc.Synopsis,
			Genre:         doc.Genre,
			AiringStatus:  doc.AiringStatus,
			Year:          doc.Year,
			Country:       doc.Country,
			Language:      doc.Language,
			PosterURL:     doc.PosterURL,
			AvgRating:     doc.AvgRating,
			ReviewCount:   doc.ReviewCount,
		}
	}
	return results, raw.EstimatedTotalHits, nil
}
