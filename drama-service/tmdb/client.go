package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	imageBase  string
	apiKey     string
	httpClient *http.Client
}

// ── Response types ────────────────────────────────────────────────────────────

type Genre struct {
	Name string `json:"name"`
}

type TVDetail struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	OriginalName     string   `json:"original_name"`
	Overview         string   `json:"overview"`
	PosterPath       string   `json:"poster_path"`
	FirstAirDate     string   `json:"first_air_date"`
	OriginCountry    []string `json:"origin_country"`
	Genres           []Genre  `json:"genres"`
	NumberOfEpisodes int      `json:"number_of_episodes"`
	Status           string   `json:"status"`
	EpisodeRunTime   []int    `json:"episode_run_time"`
}

type TVSearchResult struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	OriginalName  string   `json:"original_name"`
	Overview      string   `json:"overview"`
	PosterPath    string   `json:"poster_path"`
	FirstAirDate  string   `json:"first_air_date"`
	OriginCountry []string `json:"origin_country"`
	VoteAverage   float64  `json:"vote_average"`
}

type CreditCastMember struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	OriginalName string `json:"original_name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
}

type PersonDetail struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	AlsoKnownAs  []string `json:"also_known_as"`
	Biography    string   `json:"biography"`
	Birthday     string   `json:"birthday"`
	PlaceOfBirth string   `json:"place_of_birth"`
	ProfilePath  string   `json:"profile_path"`
}

// ── Client ────────────────────────────────────────────────────────────────────

func New(baseURL, imageBaseURL, apiKey string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		imageBase:  strings.TrimRight(imageBaseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, path string, params url.Values) (*http.Response, error) {
	params.Set("api_key", c.apiKey)
	u := fmt.Sprintf("%s%s?%s", c.baseURL, path, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// FetchTV fetches full TV show details by TMDB ID.
func (c *Client) FetchTV(ctx context.Context, id int) (*TVDetail, error) {
	resp, err := c.get(ctx, fmt.Sprintf("/3/tv/%d", id), url.Values{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tmdb: not found (id=%d)", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: unexpected status %d for id=%d", resp.StatusCode, id)
	}
	var detail TVDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("tmdb: decode tv detail: %w", err)
	}
	return &detail, nil
}

// SearchTV searches for TV shows by query string.
func (c *Client) SearchTV(ctx context.Context, query string, page int) ([]TVSearchResult, int, error) {
	if page < 1 {
		page = 1
	}
	resp, err := c.get(ctx, "/3/search/tv", url.Values{
		"query": {query},
		"page":  {fmt.Sprintf("%d", page)},
	})
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("tmdb: search returned %d", resp.StatusCode)
	}
	var result struct {
		Results      []TVSearchResult `json:"results"`
		TotalResults int              `json:"total_results"`
		TotalPages   int              `json:"total_pages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("tmdb: decode search result: %w", err)
	}
	return result.Results, result.TotalPages, nil
}

// FetchTVCredits returns the top cast for a TV show.
func (c *Client) FetchTVCredits(ctx context.Context, id int) ([]CreditCastMember, error) {
	resp, err := c.get(ctx, fmt.Sprintf("/3/tv/%d/credits", id), url.Values{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: credits returned %d for id=%d", resp.StatusCode, id)
	}
	var result struct {
		Cast []CreditCastMember `json:"cast"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("tmdb: decode credits: %w", err)
	}
	return result.Cast, nil
}

// FetchPerson returns full person details by TMDB person ID.
func (c *Client) FetchPerson(ctx context.Context, id int) (*PersonDetail, error) {
	resp, err := c.get(ctx, fmt.Sprintf("/3/person/%d", id), url.Values{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tmdb: person not found (id=%d)", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb: person returned %d for id=%d", resp.StatusCode, id)
	}
	var p PersonDetail
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("tmdb: decode person: %w", err)
	}
	return &p, nil
}

// PosterURL returns the full URL for a poster path.
func (c *Client) PosterURL(posterPath string) string {
	if posterPath == "" {
		return ""
	}
	return c.imageBase + posterPath
}

// ProfileURL returns the full URL for a person profile path.
func (c *Client) ProfileURL(profilePath string) string {
	if profilePath == "" {
		return ""
	}
	return c.imageBase + profilePath
}

// OriginCountryName maps a TMDB ISO 3166-1 alpha-2 country code to a full name.
func OriginCountryName(code string) string {
	m := map[string]string{
		"KR": "South Korea",
		"JP": "Japan",
		"CN": "China",
		"TW": "Taiwan",
		"HK": "Hong Kong",
		"TH": "Thailand",
		"US": "United States",
		"GB": "United Kingdom",
		"FR": "France",
		"PH": "Philippines",
		"VN": "Vietnam",
		"ID": "Indonesia",
		"MY": "Malaysia",
		"SG": "Singapore",
	}
	if name, ok := m[code]; ok {
		return name
	}
	return code
}
