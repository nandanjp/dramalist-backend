package elastic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"

	"dramalist/search-service/config"
)

const indexName = "catalog"

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
}

type Client struct {
	es *elasticsearch.Client
}

func New(cfg *config.Config) (*Client, error) {
	es, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{cfg.ElasticsearchURL},
	})
	if err != nil {
		return nil, fmt.Errorf("elastic: %w", err)
	}
	return &Client{es: es}, nil
}

func (c *Client) EnsureIndex(ctx context.Context) error {
	res, err := c.es.Indices.Exists([]string{indexName}, c.es.Indices.Exists.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("index exists check: %w", err)
	}
	res.Body.Close()
	if res.StatusCode == 200 {
		return nil
	}

	mapping := `{
		"mappings": {
			"properties": {
				"catalog_id":     { "type": "keyword" },
				"media_type":     { "type": "keyword" },
				"title":          { "type": "text", "analyzer": "standard" },
				"original_title": { "type": "text", "analyzer": "standard" },
				"synopsis":       { "type": "text", "analyzer": "standard" },
				"genre":          { "type": "keyword" },
				"airing_status":  { "type": "keyword" },
				"year":           { "type": "integer" },
				"country":        { "type": "keyword" },
				"language":       { "type": "keyword" },
				"poster_url":     { "type": "keyword", "index": false },
				"actor_names":    { "type": "keyword" }
			}
		}
	}`

	createRes, err := c.es.Indices.Create(
		indexName,
		c.es.Indices.Create.WithBody(strings.NewReader(mapping)),
		c.es.Indices.Create.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("index create: %w", err)
	}
	defer createRes.Body.Close()
	if createRes.IsError() {
		return fmt.Errorf("index create error: %s", createRes.String())
	}
	slog.Info("elasticsearch index created", "index", indexName)
	return nil
}

func (c *Client) CountDocuments(ctx context.Context) (int64, error) {
	res, err := c.es.Count(
		c.es.Count.WithContext(ctx),
		c.es.Count.WithIndex(indexName),
	)
	if err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return 0, fmt.Errorf("count error: %s", res.String())
	}
	var r struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return 0, err
	}
	return r.Count, nil
}

func (c *Client) IndexCatalog(ctx context.Context, doc CatalogDoc) error {
	payload, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	req := esapi.IndexRequest{
		Index:      indexName,
		DocumentID: doc.CatalogID,
		Body:       bytes.NewReader(payload),
		Refresh:    "false",
	}
	res, err := req.Do(ctx, c.es)
	if err != nil {
		return fmt.Errorf("index catalog: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("index catalog error: %s", res.String())
	}
	return nil
}

func (c *Client) DeleteCatalog(ctx context.Context, catalogID string) error {
	req := esapi.DeleteRequest{
		Index:      indexName,
		DocumentID: catalogID,
	}
	res, err := req.Do(ctx, c.es)
	if err != nil {
		return fmt.Errorf("delete catalog: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() && res.StatusCode != 404 {
		return fmt.Errorf("delete catalog error: %s", res.String())
	}
	return nil
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

func (c *Client) Search(ctx context.Context, p SearchParams) ([]SearchResult, int64, error) {
	must := []map[string]any{}
	filter := []map[string]any{}

	if p.Query != "" {
		must = append(must, map[string]any{
			"multi_match": map[string]any{
				"query":     p.Query,
				"fields":    []string{"title^3", "original_title^2", "synopsis", "genre", "actor_names"},
				"fuzziness": "AUTO",
				"type":      "best_fields",
			},
		})
	} else {
		must = append(must, map[string]any{"match_all": map[string]any{}})
	}

	if p.MediaType != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"media_type": p.MediaType}})
	}
	if p.Genre != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"genre": p.Genre}})
	}
	if p.Country != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"country": p.Country}})
	}
	if p.Language != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"language": p.Language}})
	}
	if p.AiringStatus != "" {
		filter = append(filter, map[string]any{"term": map[string]any{"airing_status": p.AiringStatus}})
	}
	if p.YearFrom > 0 || p.YearTo > 0 {
		yearRange := map[string]any{}
		if p.YearFrom > 0 {
			yearRange["gte"] = p.YearFrom
		}
		if p.YearTo > 0 {
			yearRange["lte"] = p.YearTo
		}
		filter = append(filter, map[string]any{"range": map[string]any{"year": yearRange}})
	}

	from := (p.Page - 1) * p.Limit
	body := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"must":   must,
				"filter": filter,
			},
		},
		"from": from,
		"size": p.Limit,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}

	res, err := c.es.Search(
		c.es.Search.WithContext(ctx),
		c.es.Search.WithIndex(indexName),
		c.es.Search.WithBody(bytes.NewReader(payload)),
		c.es.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("search: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return nil, 0, fmt.Errorf("search error: %s", res.String())
	}

	var raw struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source CatalogDoc `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		return nil, 0, fmt.Errorf("decode response: %w", err)
	}

	results := make([]SearchResult, 0, len(raw.Hits.Hits))
	for _, h := range raw.Hits.Hits {
		results = append(results, SearchResult{
			CatalogID:     h.Source.CatalogID,
			MediaType:     h.Source.MediaType,
			Title:         h.Source.Title,
			OriginalTitle: h.Source.OriginalTitle,
			Synopsis:      h.Source.Synopsis,
			Genre:         h.Source.Genre,
			AiringStatus:  h.Source.AiringStatus,
			Year:          h.Source.Year,
			Country:       h.Source.Country,
			Language:      h.Source.Language,
			PosterURL:     h.Source.PosterURL,
		})
	}
	return results, raw.Hits.Total.Value, nil
}
