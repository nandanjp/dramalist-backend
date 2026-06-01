package mdl

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	baseURL   = "https://mydramalist.com"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
	reLeadingInt   = regexp.MustCompile(`^(\d+)`)
	reYear         = regexp.MustCompile(`\b(19|20)\d{2}\b`)
	reDurationHr   = regexp.MustCompile(`(\d+)\s*hr`)
	reDurationMin  = regexp.MustCompile(`(\d+)\s*min`)
	reYearSuffix   = regexp.MustCompile(` \(\d{4}\)$`)
)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, url string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", url, err)
	}
	return doc, nil
}

// slugToID parses the leading integer from an MDL path like "/12345-my-drama" or "12345-my-drama".
func slugToID(path string) int {
	path = strings.TrimPrefix(path, "/")
	m := reLeadingInt.FindString(path)
	if m == "" {
		return 0
	}
	id, _ := strconv.Atoi(m)
	return id
}

func ptr[T any](v T) *T { return &v }

// imgSrc returns the first non-empty image source, checking data-src, data-cfsrc, src in order.
func imgSrc(s *goquery.Selection) string {
	for _, attr := range []string{"data-src", "data-cfsrc", "src"} {
		if v, ok := s.Attr(attr); ok && v != "" && !strings.Contains(v, "placeholder") {
			return v
		}
	}
	return ""
}

// Search scrapes the MDL search results page.
func (c *Client) Search(ctx context.Context, q string, page, limit int) ([]MDLSearchResult, bool, error) {
	if page < 1 {
		page = 1
	}
	url := fmt.Sprintf("%s/search?q=%s&adv=titles&page=%d",
		baseURL, strings.ReplaceAll(q, " ", "+"), page)

	doc, err := c.get(ctx, url)
	if err != nil {
		return nil, false, err
	}

	var results []MDLSearchResult

	// Drama/movie results are div.box elements with id="mdl-12345" inside the results column.
	doc.Find("div.col-lg-8.col-md-8 div.box").Each(func(_ int, s *goquery.Selection) {
		boxID, hasID := s.Attr("id")
		if !hasID || !strings.HasPrefix(boxID, "mdl-") {
			return
		}
		mdlID, err := strconv.Atoi(strings.TrimPrefix(boxID, "mdl-"))
		if err != nil || mdlID == 0 {
			return
		}

		titleSel := s.Find("h6.text-primary.title a").First()
		title := strings.TrimSpace(titleSel.Text())
		if title == "" {
			return
		}
		href, _ := titleSel.Attr("href")
		slug := strings.TrimPrefix(href, "/")

		r := MDLSearchResult{
			MDLID: mdlID,
			Slug:  slug,
			Title: title,
		}

		if src := imgSrc(s.Find("img.img-responsive").First()); src != "" {
			r.PosterURL = ptr(src)
		}

		// span.text-muted contains "Korean Drama - 2024, 16 episodes" or "Movie - 2024"
		typeYearText := strings.TrimSpace(s.Find("span.text-muted").First().Text())
		dashIdx := strings.Index(typeYearText, " - ")
		if dashIdx >= 0 {
			r.Type = parseTypeFromLabel(strings.TrimSpace(typeYearText[:dashIdx]))
			r.Country = parseCountryFromLabel(strings.TrimSpace(typeYearText[:dashIdx]))
			afterDash := strings.TrimSpace(typeYearText[dashIdx+3:])
			yearAndEps := strings.SplitN(afterDash, ",", 2)
			if y, err := strconv.Atoi(strings.TrimSpace(yearAndEps[0])); err == nil {
				r.Year = ptr(y)
			}
			if len(yearAndEps) == 2 {
				epsPart := strings.TrimSpace(yearAndEps[1])
				epsPart = strings.TrimSuffix(epsPart, " episodes")
				epsPart = strings.TrimSuffix(epsPart, " episode")
				if n, err := strconv.Atoi(strings.TrimSpace(epsPart)); err == nil {
					r.EpisodeCount = ptr(n)
				}
			}
		}

		if rText := strings.TrimSpace(s.Find("span.score").First().Text()); rText != "" {
			if f, err := strconv.ParseFloat(rText, 64); err == nil && f > 0 {
				r.Rating = ptr(f)
			}
		}

		results = append(results, r)
	})

	hasMore := false
	if limit > 0 && len(results) >= limit {
		hasMore = true
		results = results[:limit]
	}

	return results, hasMore, nil
}

// FetchDetail scrapes a full MDL show page using the full slug (e.g. "781538-wife-of-a-21st-century-prince").
// Passing just the numeric ID as a string also works for older shows that have MDL redirects set up.
func (c *Client) FetchDetail(ctx context.Context, slug string) (*MDLShowDetail, error) {
	url := fmt.Sprintf("%s/%s", baseURL, slug)
	doc, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}

	detail := &MDLShowDetail{MDLID: slugToID(slug), Genres: []string{}}

	detail.Title = strings.TrimSpace(doc.Find("h1.film-title").First().Text())
	if detail.Title == "" {
		detail.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	// MDL appends " (YYYY)" to disambiguate shows with the same name — strip it.
	detail.Title = reYearSuffix.ReplaceAllString(detail.Title, "")

	// film-subtitle contains "Korean Drama ‧ 2025" or "Korean Drama ‧ 12 episodes ‧ 2025"
	subTitle := strings.TrimSpace(doc.Find("div.film-subtitle").First().Text())
	if subTitle != "" {
		parts := strings.Split(subTitle, "‧")
		for i := len(parts) - 1; i >= 0; i-- {
			part := strings.TrimSpace(parts[i])
			if m := reYear.FindString(part); m != "" {
				if y, err := strconv.Atoi(m); err == nil {
					detail.Year = ptr(y)
					break
				}
			}
		}
		// Type and country from the label like "Korean Drama" (usually second-to-last part)
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if parsed := parseTypeFromLabel(part); parsed != "" && !reYear.MatchString(part) {
				if detail.Type == "" {
					detail.Type = parsed
				}
				if detail.Country == "" {
					detail.Country = parseCountryFromLabel(part)
				}
				break
			}
		}
	}

	if src := imgSrc(doc.Find(".film-poster img").First()); src != "" {
		detail.PosterURL = ptr(src)
	}

	// Synopsis: div.show-synopsis p (strip "Edit Translation" artifact)
	synopsis := strings.TrimSpace(doc.Find("div.show-synopsis p").First().Text())
	if synopsis == "" {
		synopsis = strings.TrimSpace(doc.Find("div.show-synopsis span").First().Text())
	}
	synopsis = strings.TrimSpace(strings.ReplaceAll(synopsis, "Edit Translation", ""))
	if synopsis != "" {
		detail.Synopsis = ptr(synopsis)
	}

	// Rating: div.col-film-rating div
	if rText := strings.TrimSpace(doc.Find("div.col-film-rating div").First().Text()); rText != "" {
		if f, err := strconv.ParseFloat(rText, 64); err == nil && f > 0 {
			detail.Rating = ptr(f)
		}
	}

	parseDetailRows(doc, detail)
	detail.Cast = parseCast(doc)

	return detail, nil
}

func parseDetailRows(doc *goquery.Document, detail *MDLShowDetail) {
	// MDL renders a hidden-md-up list (mobile version) with all key details.
	// Fallback: any ul.list.m-a-0 inside show-detailsxss.
	sel := doc.Find("ul.list.m-a-0.hidden-md-up li")
	if sel.Length() == 0 {
		sel = doc.Find("div.show-detailsxss ul.list.m-a-0 li")
	}

	sel.Each(func(_ int, li *goquery.Selection) {
		bSel := li.Find("b").First()
		labelRaw := strings.TrimSpace(bSel.Text()) // e.g., "Country:"
		if labelRaw == "" {
			return
		}
		label := strings.TrimSuffix(labelRaw, ":")

		// Extract value by stripping label prefix from full text (mirrors kuryana's approach).
		fullText := strings.TrimSpace(li.Text())
		value := strings.TrimSpace(strings.Replace(fullText, labelRaw+" ", "", 1))
		if value == fullText {
			value = strings.TrimSpace(strings.Replace(fullText, labelRaw, "", 1))
		}

		switch label {
		case "Country", "Country of Origin":
			if detail.Country == "" {
				detail.Country = value
			}
		case "Native Title":
			if detail.OriginalTitle == nil && value != "" {
				detail.OriginalTitle = ptr(value)
			}
		case "Type":
			if detail.Type == "" {
				detail.Type = value
			}
		case "Status", "Airing Status":
			detail.Status = value
		case "Aired":
			if detail.Status == "" {
				detail.Status = inferStatusFromAired(value)
			}
		case "Year", "Release Year":
			if detail.Year == nil {
				if y, err := strconv.Atoi(value); err == nil {
					detail.Year = ptr(y)
				} else if m := reYear.FindString(value); m != "" {
					if y, err := strconv.Atoi(m); err == nil {
						detail.Year = ptr(y)
					}
				}
			}
		case "Episodes":
			if detail.EpisodeCount == nil {
				if fields := strings.Fields(value); len(fields) > 0 {
					if n, err := strconv.Atoi(fields[0]); err == nil {
						detail.EpisodeCount = ptr(n)
					}
				}
			}
		case "Duration":
			if detail.Duration == nil {
				if d := parseDuration(value); d > 0 {
					detail.Duration = ptr(d)
				}
			}
		case "Score", "Rating":
			if detail.Rating == nil && value != "N/A" {
				if fields := strings.Fields(value); len(fields) > 0 {
					if f, err := strconv.ParseFloat(fields[0], 64); err == nil && f > 0 {
						detail.Rating = ptr(f)
					}
				}
			}
		case "Genres", "Genre", "Tags":
			li.Find("a").Each(func(_ int, a *goquery.Selection) {
				if g := strings.TrimSpace(a.Text()); g != "" {
					detail.Genres = append(detail.Genres, g)
				}
			})
		}
	})

	// Fallback genre extraction from li.show-genres (desktop details section).
	if len(detail.Genres) == 0 {
		doc.Find("li.show-genres a.text-primary").Each(func(_ int, a *goquery.Selection) {
			if g := strings.TrimSpace(a.Text()); g != "" {
				detail.Genres = append(detail.Genres, g)
			}
		})
	}

	if detail.Status == "" {
		detail.Status = "completed"
	}
}

// parseDuration converts MDL duration strings to total minutes.
// Handles: "1 hr. 10 min." → 70, "45 min." → 45, "1 hr." → 60.
func parseDuration(s string) int {
	total := 0
	if m := reDurationHr.FindStringSubmatch(s); len(m) > 1 {
		if h, err := strconv.Atoi(m[1]); err == nil {
			total += h * 60
		}
	}
	if m := reDurationMin.FindStringSubmatch(s); len(m) > 1 {
		if min, err := strconv.Atoi(m[1]); err == nil {
			total += min
		}
	}
	return total
}

// inferStatusFromAired infers airing_status from the "Aired:" field value.
// e.g. "Nov 29, 2024 - Jan 18, 2025" → "completed"; "Nov 29, 2024 - ?" → "ongoing".
func inferStatusFromAired(aired string) string {
	aired = strings.TrimSpace(aired)
	if aired == "" || aired == "N/A" || aired == "TBA" || aired == "Not Yet Aired" {
		return "upcoming"
	}
	parts := strings.Split(aired, " - ")
	if len(parts) == 1 {
		t, err := time.Parse("Jan 2, 2006", strings.TrimSpace(parts[0]))
		if err == nil && t.After(time.Now()) {
			return "upcoming"
		}
		return "completed"
	}
	endDate := strings.TrimSpace(parts[1])
	if endDate == "?" || endDate == "" || endDate == "TBA" {
		return "ongoing"
	}
	t, err := time.Parse("Jan 2, 2006", endDate)
	if err != nil {
		return "completed"
	}
	if t.After(time.Now()) {
		return "ongoing"
	}
	return "completed"
}

func parseCast(doc *goquery.Document) []MDLCastMember {
	var cast []MDLCastMember

	// Each cast entry is li.list-item.col-sm-4 anywhere on the page (cast credits section).
	doc.Find("li.list-item.col-sm-4").Each(func(_ int, li *goquery.Selection) {
		castLink := li.Find("a.text-primary.text-ellipsis").First()
		if castLink.Length() == 0 {
			return
		}

		href, _ := castLink.Attr("href") // "/people/426-iu"
		personID := slugToID(strings.TrimPrefix(href, "/people/"))
		if personID == 0 {
			slog.Warn("drama scraper: cast member missing person ID", "href", href)
			return
		}

		name := strings.TrimSpace(castLink.Find("b").Text())
		if name == "" {
			name = strings.TrimSpace(castLink.Text())
		}
		if name == "" {
			slog.Warn("drama scraper: cast member has no name, skipping")
			return
		}

		// Character name: div.text-ellipsis > small > a
		characterName := strings.TrimSpace(li.Find("div.text-ellipsis small a").First().Text())
		if characterName == "" {
			li.Find("small").Each(func(_ int, sm *goquery.Selection) {
				if characterName != "" {
					return
				}
				if !sm.HasClass("text-muted") {
					if t := strings.TrimSpace(sm.Text()); t != "" {
						characterName = t
					}
				}
			})
		}

		role := strings.TrimSpace(li.Find("small.text-muted").First().Text())
		photoURL := imgSrc(li.Find("img").First())

		cast = append(cast, MDLCastMember{
			PersonID:      personID,
			Name:          name,
			CharacterName: characterName,
			Role:          role,
			PhotoURL:      photoURL,
		})
	})

	if len(cast) == 0 {
		slog.Warn("drama scraper: no cast found — selector may need updating")
	}
	return cast
}

// parseTypeFromLabel extracts the bare drama type from labels like "Korean Drama" → "Drama".
func parseTypeFromLabel(label string) string {
	knownPrefixes := []string{
		"Korean ", "Japanese ", "Chinese ", "Taiwanese ", "Thai ", "Filipino ",
		"Vietnamese ", "Hong Kong ", "Singaporean ", "Indonesian ", "Malaysian ",
		"American ", "British ", "French ", "Spanish ", "Indian ",
	}
	for _, p := range knownPrefixes {
		if strings.HasPrefix(label, p) {
			return strings.TrimPrefix(label, p)
		}
	}
	knownTypes := map[string]bool{
		"Drama": true, "Movie": true, "Special": true, "Mini-Series": true,
		"Web Series": true, "TV Series": true,
	}
	if knownTypes[label] {
		return label
	}
	return ""
}

// FetchPerson scrapes an MDL people page (e.g. "900-lee-jong-suk").
func (c *Client) FetchPerson(ctx context.Context, slug string) (*MDLPersonDetail, error) {
	doc, err := c.get(ctx, baseURL+"/people/"+slug)
	if err != nil {
		return nil, err
	}

	p := &MDLPersonDetail{
		PersonID: slugToID(slug),
		Slug:     slug,
	}

	// Name
	name := strings.TrimSpace(doc.Find("h1.film-title").First().Text())
	name = reYearSuffix.ReplaceAllString(name, "")
	if name == "" {
		return nil, fmt.Errorf("FetchPerson: no name found for %q", slug)
	}
	p.Name = name

	// Profile image — look in the left column poster area
	imgSel := doc.Find("div.col-lg-4.col-md-4 img, div.film-content img, div.col-xs-4 img").First()
	if src := imgSrc(imgSel); src != "" {
		p.ProfileURL = &src
	}

	// Biography — the text block in the main column minus the mobile detail list
	bioContainer := doc.Find("div.col-lg-8.col-md-8 div.col-sm-8.col-lg-12.col-md-12").First()
	if bioContainer.Length() > 0 {
		// Remove the hidden detail list so we only get prose text
		bioContainer.Find("div.hidden-md-up, ul.list").Each(func(_ int, s *goquery.Selection) {
			s.Remove()
		})
		bio := strings.TrimSpace(bioContainer.Text())
		if bio != "" {
			p.Biography = &bio
		}
	}

	// Detail rows: ul.list.m-b-0 li (person pages use m-b-0, show pages use m-a-0 hidden-md-up)
	doc.Find("ul.list.m-b-0 li").Each(func(_ int, li *goquery.Selection) {
		labelRaw := strings.TrimSpace(li.Find("b").First().Text())
		value := strings.TrimSpace(strings.Replace(li.Text(), labelRaw+" ", "", 1))
		if value == strings.TrimSpace(li.Text()) {
			value = strings.TrimSpace(strings.Replace(li.Text(), labelRaw, "", 1))
		}
		label := strings.TrimSuffix(labelRaw, ":")

		switch strings.ToLower(label) {
		case "nationality":
			if value != "" {
				p.Nationality = &value
			}
		case "also known as", "native name":
			// Take first entry (before comma) as the native name
			parts := strings.SplitN(value, ",", 2)
			n := strings.TrimSpace(parts[0])
			if n != "" {
				p.NativeName = &n
			}
		case "born":
			// Format: "Sep 14, 1987 (age 37)" — extract date
			if bd := parseBirthdate(value); bd != "" {
				p.Birthdate = &bd
			}
		}
	})

	return p, nil
}

var reBirthdate = regexp.MustCompile(`([A-Za-z]+)\s+(\d{1,2}),?\s+(\d{4})`)

// parseBirthdate converts "Sep 14, 1987 (age 37)" → "1987-09-14".
func parseBirthdate(s string) string {
	m := reBirthdate.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	months := map[string]string{
		"jan": "01", "feb": "02", "mar": "03", "apr": "04",
		"may": "05", "jun": "06", "jul": "07", "aug": "08",
		"sep": "09", "oct": "10", "nov": "11", "dec": "12",
	}
	mo, ok := months[strings.ToLower(m[1])[:3]]
	if !ok {
		return ""
	}
	day := fmt.Sprintf("%02s", m[2])
	return m[3] + "-" + mo + "-" + day
}

// parseCountryFromLabel extracts country from labels like "Korean Drama" → "South Korea".
func parseCountryFromLabel(label string) string {
	switch {
	case strings.HasPrefix(label, "Korean"):
		return "South Korea"
	case strings.HasPrefix(label, "Japanese"):
		return "Japan"
	case strings.HasPrefix(label, "Chinese"):
		return "China"
	case strings.HasPrefix(label, "Taiwanese"):
		return "Taiwan"
	case strings.HasPrefix(label, "Thai"):
		return "Thailand"
	case strings.HasPrefix(label, "Filipino"):
		return "Philippines"
	case strings.HasPrefix(label, "Vietnamese"):
		return "Vietnam"
	case strings.HasPrefix(label, "Hong Kong"):
		return "Hong Kong"
	case strings.HasPrefix(label, "Indonesian"):
		return "Indonesia"
	case strings.HasPrefix(label, "Malaysian"):
		return "Malaysia"
	default:
		return ""
	}
}
