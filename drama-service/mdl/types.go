package mdl

type MDLCastMember struct {
	PersonID      int
	Name          string
	CharacterName string
	Role          string // raw: "Main Role", "Support Role", "Guest Role"
	PhotoURL      string
}

type MDLSearchResult struct {
	MDLID        int
	Slug         string
	Title        string
	PosterURL    *string
	Year         *int
	Type         string // "Drama", "Movie", "Special", etc.
	Country      string
	Rating       *float64
	EpisodeCount *int
}

type MDLShowDetail struct {
	MDLID         int
	Title         string
	OriginalTitle *string
	PosterURL     *string
	Year          *int
	Type          string
	Country       string
	Rating        *float64
	EpisodeCount  *int
	Duration      *int // minutes
	Synopsis      *string
	Status        string // "Ongoing", "Completed", "Upcoming", etc.
	Genres        []string
	Cast          []MDLCastMember
}
