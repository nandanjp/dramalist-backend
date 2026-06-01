package anilist

import "strings"

func StatusToAiringStatus(s string) string {
	switch s {
	case "RELEASING":
		return "ongoing"
	case "NOT_YET_RELEASED":
		return "upcoming"
	default:
		return "completed"
	}
}

func CountryToLanguage(country *string) *string {
	if country == nil {
		return nil
	}
	var lang string
	switch *country {
	case "JP":
		lang = "Japanese"
	case "KR":
		lang = "Korean"
	case "CN", "TW":
		lang = "Chinese"
	default:
		return nil
	}
	return &lang
}

func MapCharacterRole(role string) string {
	switch role {
	case "MAIN":
		return "main"
	case "SUPPORTING":
		return "supporting"
	default:
		return "guest"
	}
}

func (m *ALMedia) PrimaryTitle() string {
	if m.Title.English != nil && *m.Title.English != "" {
		return *m.Title.English
	}
	if m.Title.Romaji != nil && *m.Title.Romaji != "" {
		return *m.Title.Romaji
	}
	if m.Title.Native != nil {
		return *m.Title.Native
	}
	return ""
}

func (m *ALMedia) Studio() *string {
	if m.Studios == nil || len(m.Studios.Nodes) == 0 {
		return nil
	}
	s := m.Studios.Nodes[0].Name
	return &s
}

func (m *ALMedia) LowercaseGenres() []string {
	genres := make([]string, 0, len(m.Genres))
	for _, g := range m.Genres {
		genres = append(genres, strings.ToLower(g))
	}
	return genres
}
