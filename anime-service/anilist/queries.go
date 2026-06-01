package anilist

const searchQuery = `
query ($search: String, $page: Int, $perPage: Int) {
  Page(page: $page, perPage: $perPage) {
    pageInfo {
      total
      currentPage
      lastPage
      hasNextPage
    }
    media(search: $search, type: ANIME, sort: SEARCH_MATCH) {
      id
      title { romaji english native }
      coverImage { large }
      startDate { year }
      episodes
      duration
      genres
      status
      format
      averageScore
      description(asHtml: false)
    }
  }
}
`

const fetchByIDQuery = `
query ($id: Int) {
  Media(id: $id, type: ANIME) {
    id
    title { romaji english native }
    coverImage { large }
    startDate { year }
    episodes
    duration
    genres
    status
    format
    averageScore
    description(asHtml: false)
    countryOfOrigin
    studios(isMain: true) { nodes { name } }
    characters(sort: [ROLE], perPage: 25) {
      edges {
        role
        node {
          id
          name { full native }
          image { large }
        }
        voiceActors(language: JAPANESE) {
          id
          name { full native }
          image { large }
          dateOfBirth { year month day }
        }
      }
    }
  }
}
`

// ── Response types ────────────────────────────────────────────────────────────

type PageInfo struct {
	Total       int  `json:"total"`
	CurrentPage int  `json:"currentPage"`
	LastPage    int  `json:"lastPage"`
	HasNextPage bool `json:"hasNextPage"`
}

type ALTitle struct {
	Romaji  *string `json:"romaji"`
	English *string `json:"english"`
	Native  *string `json:"native"`
}

type ALCoverImage struct {
	Large *string `json:"large"`
}

type ALDate struct {
	Year  *int `json:"year"`
	Month *int `json:"month"`
	Day   *int `json:"day"`
}

type ALMedia struct {
	ID              int                    `json:"id"`
	Title           ALTitle                `json:"title"`
	CoverImage      ALCoverImage           `json:"coverImage"`
	StartDate       ALDate                 `json:"startDate"`
	Episodes        *int                   `json:"episodes"`
	Duration        *int                   `json:"duration"`
	Genres          []string               `json:"genres"`
	Status          string                 `json:"status"`
	Format          string                 `json:"format"`
	AverageScore    *int                   `json:"averageScore"`
	Description     *string                `json:"description"`
	CountryOfOrigin *string                `json:"countryOfOrigin"`
	Studios         *ALStudios             `json:"studios"`
	Characters      *ALCharacterConnection `json:"characters"`
}

type ALStudios struct {
	Nodes []ALStudio `json:"nodes"`
}

type ALStudio struct {
	Name string `json:"name"`
}

type ALCharacterConnection struct {
	Edges []ALCharacterEdge `json:"edges"`
}

type ALCharacterEdge struct {
	Role        string     `json:"role"`
	Node        ALCharacter `json:"node"`
	VoiceActors []ALPerson `json:"voiceActors"`
}

type ALCharacter struct {
	ID    int     `json:"id"`
	Name  ALName  `json:"name"`
	Image ALImage `json:"image"`
}

type ALPerson struct {
	ID          int     `json:"id"`
	Name        ALName  `json:"name"`
	Image       ALImage `json:"image"`
	DateOfBirth ALDate  `json:"dateOfBirth"`
}

type ALName struct {
	Full   *string `json:"full"`
	Native *string `json:"native"`
}

type ALImage struct {
	Large *string `json:"large"`
}

type ALError struct {
	Message string `json:"message"`
}

type searchResponse struct {
	Data struct {
		Page struct {
			PageInfo PageInfo  `json:"pageInfo"`
			Media    []ALMedia `json:"media"`
		} `json:"Page"`
	} `json:"data"`
	Errors []ALError `json:"errors"`
}

type fetchResponse struct {
	Data struct {
		Media ALMedia `json:"Media"`
	} `json:"data"`
	Errors []ALError `json:"errors"`
}
