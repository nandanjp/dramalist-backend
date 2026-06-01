package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const anilistEndpoint = "https://graphql.anilist.co"

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anilistEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("anilist request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anilist status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode anilist response: %w", err)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, q string, page, perPage int) (*PageInfo, []ALMedia, error) {
	var resp searchResponse
	if err := c.do(ctx, searchQuery, map[string]any{
		"search":  q,
		"page":    page,
		"perPage": perPage,
	}, &resp); err != nil {
		return nil, nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, nil, fmt.Errorf("anilist: %s", resp.Errors[0].Message)
	}
	pi := resp.Data.Page.PageInfo
	return &pi, resp.Data.Page.Media, nil
}

func (c *Client) FetchByID(ctx context.Context, id int) (*ALMedia, error) {
	var resp fetchResponse
	if err := c.do(ctx, fetchByIDQuery, map[string]any{"id": id}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("anilist: %s", resp.Errors[0].Message)
	}
	m := resp.Data.Media
	return &m, nil
}
