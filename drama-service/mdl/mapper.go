package mdl

import "strings"

func StatusToAiringStatus(s string) string {
	switch s {
	case "Ongoing", "Airing":
		return "ongoing"
	case "Upcoming", "Not Yet Aired":
		return "upcoming"
	default:
		return "completed"
	}
}

func TypeToMediaType(s string) string {
	switch s {
	case "Movie":
		return "movie"
	default: // "Drama", "Special", "Mini-Series", "Web Series", etc.
		return "show"
	}
}

func CountryToLanguage(country string) *string {
	m := map[string]string{
		"South Korea":      "Korean",
		"Korea":            "Korean",
		"Japan":            "Japanese",
		"China":            "Chinese",
		"China (Mainland)": "Chinese",
		"Taiwan":           "Chinese",
		"Thailand":         "Thai",
		"Philippines":      "Filipino",
		"Hong Kong":        "Cantonese",
		"Vietnam":          "Vietnamese",
		"Indonesia":        "Indonesian",
		"Malaysia":         "Malay",
	}
	if lang, ok := m[country]; ok {
		return &lang
	}
	return nil
}

func MapCastRole(raw string) string {
	switch raw {
	case "Main Role":
		return "main"
	case "Support Role":
		return "supporting"
	default:
		return "guest"
	}
}

func LowercaseGenres(genres []string) []string {
	out := make([]string, len(genres))
	for i, g := range genres {
		out[i] = strings.ToLower(g)
	}
	return out
}
