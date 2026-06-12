// stub-server — deterministic HTTP stub for TMDB, AniList, and Ollama.
// Used exclusively in the smoke test docker-compose stack.
// All responses are hardcoded fixtures keyed on well-known IDs.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Well-known fixture IDs that smoke tests use.
const (
	TMDBDramaID   = "98765"
	TMDBThrowaway = "99999"
	AniListAnimeID = "11111"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8089"
	}

	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok","service":"stub-server"}`)
	})

	// Stub poster image — valid 1×1 white JPEG
	mux.HandleFunc("GET /stub-poster.jpg", serveStubImage)

	// ── TMDB ─────────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /3/search/tv",    tmdbSearchTV)
	mux.HandleFunc("GET /3/search/movie", tmdbSearchMovie)
	mux.HandleFunc("GET /3/tv/{id}/credits", tmdbTVCredits)
	mux.HandleFunc("GET /3/tv/",          tmdbTVDetail)
	mux.HandleFunc("GET /3/movie/",       tmdbMovieDetail)
	mux.HandleFunc("GET /3/person/",      tmdbPersonDetail)

	// ── AniList (GraphQL) ────────────────────────────────────────────────────
	mux.HandleFunc("POST /", aniListGraphQL)

	// ── Ollama ───────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /api/tags",      ollamaTags)
	mux.HandleFunc("POST /api/generate", ollamaGenerate)
	mux.HandleFunc("POST /api/chat",     ollamaChat)

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	slog.Info("stub-server starting", "port", port)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("stub-server error", "err", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// serveStubImage returns a valid 1×1 white JPEG.
func serveStubImage(w http.ResponseWriter, r *http.Request) {
	jpeg := []byte{
		0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x84, 0x00, 0x05, 0x03, 0x04, 0x04, 0x04,
		0x03, 0x05, 0x04, 0x04, 0x04, 0x05, 0x05, 0x05, 0x06, 0x07, 0x0C, 0x08,
		0x07, 0x07, 0x07, 0x07, 0x0F, 0x0B, 0x0B, 0x09, 0x0C, 0x11, 0x0F, 0x12,
		0x12, 0x11, 0x0F, 0x11, 0x11, 0x13, 0x16, 0x1C, 0x17, 0x13, 0x14, 0x1A,
		0x15, 0x11, 0x11, 0x18, 0x21, 0x18, 0x1A, 0x1D, 0x1D, 0x1F, 0x1F, 0x1F,
		0x13, 0x17, 0x22, 0x24, 0x22, 0x1E, 0x24, 0x1C, 0x1E, 0x1F, 0x1E, 0x01,
		0x05, 0x05, 0x05, 0x07, 0x06, 0x07, 0x0E, 0x08, 0x08, 0x0E, 0x1E, 0x14,
		0x11, 0x14, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E,
		0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E,
		0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E,
		0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E, 0x1E,
		0x1E, 0x1E, 0x1E, 0x1E, 0xFF, 0xC0, 0x00, 0x11, 0x08, 0x00, 0x01, 0x00,
		0x01, 0x03, 0x01, 0x22, 0x00, 0x02, 0x11, 0x01, 0x03, 0x11, 0x01, 0xFF,
		0xC4, 0x01, 0xA2, 0x00, 0x00, 0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03,
		0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x10, 0x00, 0x02, 0x01,
		0x03, 0x03, 0x02, 0x04, 0x03, 0x05, 0x05, 0x04, 0x04, 0x00, 0x00, 0x01,
		0x7D, 0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12, 0x21, 0x31, 0x41,
		0x06, 0x13, 0x51, 0x61, 0x07, 0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xA1,
		0x08, 0x23, 0x42, 0xB1, 0xC1, 0x15, 0x52, 0xD1, 0xF0, 0x24, 0x33, 0x62,
		0x72, 0x82, 0x09, 0x0A, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2A, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x43, 0x44,
		0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
		0x59, 0x5A, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6A, 0x73, 0x74,
		0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88,
		0x89, 0x8A, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0xA2,
		0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8, 0xA9, 0xAA, 0xB2, 0xB3, 0xB4, 0xB5,
		0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xC2, 0xC3, 0xC4, 0xC5, 0xC6, 0xC7, 0xC8,
		0xC9, 0xCA, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xE1,
		0xE2, 0xE3, 0xE4, 0xE5, 0xE6, 0xE7, 0xE8, 0xE9, 0xEA, 0xF1, 0xF2, 0xF3,
		0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9, 0xFA, 0x01, 0x00, 0x03, 0x01, 0x01,
		0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B,
		0x11, 0x00, 0x02, 0x01, 0x02, 0x04, 0x04, 0x03, 0x04, 0x07, 0x05, 0x04,
		0x04, 0x00, 0x01, 0x02, 0x77, 0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05,
		0x21, 0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71, 0x13, 0x22, 0x32,
		0x81, 0x08, 0x14, 0x42, 0x91, 0xA1, 0xB1, 0xC1, 0x09, 0x23, 0x33, 0x52,
		0xF0, 0x15, 0x62, 0x72, 0xD1, 0x0A, 0x16, 0x24, 0x34, 0xE1, 0x25, 0xF1,
		0x17, 0x18, 0x19, 0x1A, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x35, 0x36, 0x37,
		0x38, 0x39, 0x3A, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49, 0x4A, 0x53,
		0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x63, 0x64, 0x65, 0x66, 0x67,
		0x68, 0x69, 0x6A, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x82,
		0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8A, 0x92, 0x93, 0x94, 0x95,
		0x96, 0x97, 0x98, 0x99, 0x9A, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8,
		0xA9, 0xAA, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xC2,
		0xC3, 0xC4, 0xC5, 0xC6, 0xC7, 0xC8, 0xC9, 0xCA, 0xD2, 0xD3, 0xD4, 0xD5,
		0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xE2, 0xE3, 0xE4, 0xE5, 0xE6, 0xE7, 0xE8,
		0xE9, 0xEA, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9, 0xFA, 0xFF,
		0xDA, 0x00, 0x0C, 0x03, 0x01, 0x00, 0x02, 0x11, 0x03, 0x11, 0x00, 0x3F,
		0x00, 0xFB, 0x2E, 0x8A, 0x28, 0xA0, 0x0F, 0xFF, 0xD9,
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(jpeg)
}

// ── TMDB stubs ────────────────────────────────────────────────────────────────

func tmdbSearchTV(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"results": []map[string]any{
			{
				"id":             98765,
				"name":           "Smoke Test Drama",
				"original_name":  "스모크 테스트 드라마",
				"overview":       "A deterministic smoke test Korean drama.",
				"poster_path":    "/stub-poster.jpg",
				"first_air_date": "2024-01-15",
				"origin_country": []string{"KR"},
				"vote_average":   8.4,
			},
		},
		"total_results": 1,
		"total_pages":   1,
		"page":          1,
	})
}

func tmdbSearchMovie(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"results":       []any{},
		"total_results": 0,
		"total_pages":   0,
		"page":          1,
	})
}

func tmdbTVDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/3/tv/")
	id = strings.Split(id, "?")[0]

	switch id {
	case TMDBDramaID:
		writeJSON(w, map[string]any{
			"id":                 98765,
			"name":               "Smoke Test Drama",
			"original_name":      "스모크 테스트 드라마",
			"overview":           "A deterministic smoke test Korean drama.",
			"poster_path":        "/stub-poster.jpg",
			"first_air_date":     "2024-01-15",
			"origin_country":     []string{"KR"},
			"genres":             []map[string]any{{"id": 18, "name": "Drama"}},
			"number_of_episodes": 16,
			"status":             "Ended",
			"episode_run_time":   []int{60},
			"vote_average":       8.4,
		})
	case TMDBThrowaway:
		writeJSON(w, map[string]any{
			"id":                 99999,
			"name":               "Throwaway Smoke Test Show",
			"original_name":      "Throwaway Smoke Test Show",
			"overview":           "A throwaway entry used by the deletion smoke test.",
			"poster_path":        "/stub-poster.jpg",
			"first_air_date":     "2023-06-01",
			"origin_country":     []string{"KR"},
			"genres":             []map[string]any{{"id": 18, "name": "Drama"}},
			"number_of_episodes": 8,
			"status":             "Ended",
			"episode_run_time":   []int{60},
			"vote_average":       7.0,
		})
	default:
		http.NotFound(w, r)
	}
}

func tmdbMovieDetail(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func tmdbTVCredits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"cast": []map[string]any{
			{
				"id":            55001,
				"name":          "Smoke Actor One",
				"original_name": "스모크 배우 일",
				"character":     "Lead Character",
				"profile_path":  "/stub-poster.jpg",
				"order":         0,
			},
			{
				"id":            55002,
				"name":          "Smoke Actor Two",
				"original_name": "스모크 배우 이",
				"character":     "Supporting Character",
				"profile_path":  "/stub-poster.jpg",
				"order":         1,
			},
		},
	})
}

func tmdbPersonDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/3/person/")
	id = strings.Split(id, "?")[0]

	switch id {
	case "55001":
		writeJSON(w, map[string]any{
			"id":             55001,
			"name":           "Smoke Actor One",
			"also_known_as":  []string{"스모크 배우 일"},
			"biography":      "A fictional actor for smoke testing.",
			"birthday":       "1990-01-01",
			"place_of_birth": "Seoul, South Korea",
			"profile_path":   "/stub-poster.jpg",
		})
	default:
		writeJSON(w, map[string]any{
			"id":             0,
			"name":           "Unknown Actor",
			"also_known_as":  []string{},
			"biography":      "",
			"birthday":       "",
			"place_of_birth": "",
			"profile_path":   "",
		})
	}
}

// ── AniList stub ─────────────────────────────────────────────────────────────

func aniListGraphQL(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)

	query, _ := body["query"].(string)

	if strings.Contains(query, "Page") {
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"Page": map[string]any{
					"media": []map[string]any{
						{
							"id": 11111,
							"title": map[string]any{
								"romaji":  "Smoke Test Anime",
								"english": "Smoke Test Anime",
								"native":  "スモークテストアニメ",
							},
							"coverImage": map[string]any{
								"extraLarge": "http://stub-server:8089/stub-poster.jpg",
							},
							"startDate":       map[string]any{"year": 2024, "month": 1, "day": 1},
							"episodes":        12,
							"genres":          []string{"Action"},
							"status":          "FINISHED",
							"countryOfOrigin": "JP",
							"averageScore":    84,
						},
					},
				},
			},
		})
		return
	}

	writeJSON(w, map[string]any{
		"data": map[string]any{
			"Media": map[string]any{
				"id": 11111,
				"title": map[string]any{
					"romaji":  "Smoke Test Anime",
					"english": "Smoke Test Anime",
					"native":  "スモークテストアニメ",
				},
				"description": "A deterministic smoke test anime.",
				"coverImage": map[string]any{
					"extraLarge": "http://stub-server:8089/stub-poster.jpg",
				},
				"startDate":       map[string]any{"year": 2024, "month": 1, "day": 1},
				"episodes":        12,
				"genres":          []string{"Action"},
				"status":          "FINISHED",
				"countryOfOrigin": "JP",
				"averageScore":    84,
			},
		},
	})
}

// ── Ollama stubs ──────────────────────────────────────────────────────────────

func ollamaTags(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"models": []map[string]any{
			{"name": "llama3.2:3b", "model": "llama3.2:3b"},
		},
	})
}

func ollamaGenerate(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)

	stream, _ := req["stream"].(bool)
	if !stream {
		writeJSON(w, map[string]any{
			"model":    "llama3.2:3b",
			"response": "Based on your watching history, I recommend Signal (2016) — a gripping Korean thriller.",
			"done":     true,
		})
		return
	}

	tokens := []string{
		"Based ", "on your ", "watching ", "history, ",
		"I recommend ", "Signal ", "(2016) ", "— a ", "gripping ",
		"Korean ", "thriller.", "",
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Transfer-Encoding", "chunked")
	flusher, _ := w.(http.Flusher)

	for i, token := range tokens {
		done := i == len(tokens)-1
		chunk := map[string]any{
			"model":    "llama3.2:3b",
			"response": token,
			"done":     done,
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "%s\n", data)
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ollamaChat handles POST /api/chat (non-streaming only).
func ollamaChat(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"model": "llama3.2:3b",
		"message": map[string]any{
			"role":    "assistant",
			"content": "Based on your watching history, I recommend Signal (2016) — a gripping Korean thriller with time-travel elements and an intense storyline.",
		},
		"done": true,
	})
}
