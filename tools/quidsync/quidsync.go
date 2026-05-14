package quidsync

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	wpPostsURLFmt   = "https://quid.mensa.it/wp-json/wp/v2/posts?categories=%d&per_page=100&page=%d&_embed=wp:featuredmedia&_fields=id,slug,link,date,title,excerpt,content,categories,_links,_embedded"
	wpCategoriesURL = "https://quid.mensa.it/wp-json/wp/v2/categories?per_page=100&_fields=id,slug,name,count"
	defaultTimeout  = 30 * time.Second
	maxBodyChars    = 60000
	maxExcerptChars = 4000
)

// IssueSlugRE matcha slug WP del tipo "quid-16-la-fine".
var IssueSlugRE = regexp.MustCompile(`^quid-(\d+)-`)

type wpCategory struct {
	ID    int    `json:"id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// IssueCategory rappresenta una categoria che e` un numero Quid.
type IssueCategory struct {
	ID     int
	Number int
	Name   string
	Slug   string
	Count  int
}

// FetchIssueCategories ritorna tutte le categorie WP che rappresentano un
// numero Quid (slug `quid-N-...`), ordinate per numero decrescente.
func FetchIssueCategories() ([]IssueCategory, error) {
	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Get(wpCategoriesURL)
	if err != nil {
		return nil, fmt.Errorf("GET categories: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET categories: status %d", resp.StatusCode)
	}
	var cats []wpCategory
	if err := json.NewDecoder(resp.Body).Decode(&cats); err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	out := make([]IssueCategory, 0, len(cats))
	for _, c := range cats {
		m := IssueSlugRE.FindStringSubmatch(c.Slug)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		out = append(out, IssueCategory{ID: c.ID, Number: n, Name: c.Name, Slug: c.Slug, Count: c.Count})
	}
	// Sort decrescente per Number.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Number > out[i].Number {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// SyncAllIssues e` la versione full-history: scarica e upserta gli articoli
// di tutti i numeri Quid noti (categorie WP) + i numeri storici disponibili
// solo come PDF (1-12) scrappati da /archivio-quid/.
// Usata dalla CLI quid-backfill al primo deploy e in disaster recovery.
func SyncAllIssues(app core.App) (perIssue map[int]int, err error) {
	cats, err := FetchIssueCategories()
	if err != nil {
		return nil, err
	}
	perIssue = make(map[int]int, len(cats))
	for _, c := range cats {
		count, err := SyncIssue(app, c)
		if err != nil {
			app.Logger().Error("[quidsync] sync issue fallito, continuo con i successivi",
				"issue", c.Number, "id", c.ID, "err", err)
			continue
		}
		perIssue[c.Number] = count
		app.Logger().Info("[quidsync] issue sincronizzato", "issue", c.Number, "name", c.Name, "articles", count)
	}

	// Storico in PDF: numeri presenti solo su /archivio-quid/ (1-12 al momento).
	pdfCount, err := SyncArchive(app)
	if err != nil {
		app.Logger().Error("[quidsync] sync archive PDF fallito", "err", err)
	} else {
		app.Logger().Info("[quidsync] archive PDF sincronizzato", "issues", pdfCount)
	}

	return perIssue, nil
}

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)
var whitespaceRE = regexp.MustCompile(`\s+`)

type wpRendered struct {
	Rendered string `json:"rendered"`
}

type wpEmbeddedMedia struct {
	SourceURL string `json:"source_url"`
}

type wpEmbedded struct {
	FeaturedMedia []wpEmbeddedMedia `json:"wp:featuredmedia"`
}

type wpPost struct {
	ID         int        `json:"id"`
	Slug       string     `json:"slug"`
	Link       string     `json:"link"`
	Date       string     `json:"date"`
	Title      wpRendered `json:"title"`
	Excerpt    wpRendered `json:"excerpt"`
	Content    wpRendered `json:"content"`
	Categories []int      `json:"categories"`
	Embedded   wpEmbedded `json:"_embedded"`
}

// SyncIssue scarica tutti gli articoli del numero (categoria WP) indicato e
// li upserta nella collection `quid_articles`. Dopo gli articoli, upserta
// anche il record del numero stesso in `quid_issues` (con immagine presa dal
// primo articolo trovato). L'hook registrato in main/hooks si occupa di
// riflettere ogni write nell'indice Bleve.
//
// categoryName e numero servono per identificare il numero in modo leggibile
// nei risultati di search.
func SyncIssue(app core.App, cat IssueCategory) (int, error) {
	articlesCol, err := app.FindCollectionByNameOrId("quid_articles")
	if err != nil {
		return 0, fmt.Errorf("find collection quid_articles: %w", err)
	}

	total := 0
	var coverImage string
	var firstPublishedAt time.Time
	for page := 1; page <= 50; page++ {
		posts, err := fetchPostsPage(cat.ID, page)
		if err != nil {
			return total, fmt.Errorf("fetch page %d: %w", page, err)
		}
		if len(posts) == 0 {
			break
		}
		for _, p := range posts {
			if coverImage == "" {
				coverImage = featuredImageURL(&p)
			}
			if t, err := time.Parse("2006-01-02T15:04:05", p.Date); err == nil {
				if firstPublishedAt.IsZero() || t.Before(firstPublishedAt) {
					firstPublishedAt = t
				}
			}
			if err := upsertPost(app, articlesCol, &p, cat.ID, cat.Name); err != nil {
				app.Logger().Error("[quidsync] upsert post fallito", "wp_id", p.ID, "err", err)
				continue
			}
			total++
		}
		if len(posts) < 100 {
			break
		}
	}

	if err := upsertIssue(app, cat, total, coverImage, firstPublishedAt); err != nil {
		app.Logger().Error("[quidsync] upsert issue fallito", "category_id", cat.ID, "err", err)
	}

	return total, nil
}

func upsertIssue(app core.App, cat IssueCategory, articlesCount int, image string, publishedAt time.Time) error {
	collection, err := app.FindCollectionByNameOrId("quid_issues")
	if err != nil {
		return fmt.Errorf("find collection quid_issues: %w", err)
	}
	categoryID := strconv.Itoa(cat.ID)
	rec, err := app.FindFirstRecordByData(collection.Id, "category_id", categoryID)
	if err != nil || rec == nil {
		rec = core.NewRecord(collection)
		rec.Set("category_id", categoryID)
	}
	rec.Set("number", cat.Number)
	rec.Set("name", cat.Name)
	rec.Set("slug", cat.Slug)
	rec.Set("articles_count", articlesCount)
	if image != "" {
		rec.Set("image", image)
	}
	if !publishedAt.IsZero() {
		rec.Set("published_at", publishedAt)
	}
	return app.Save(rec)
}

func fetchPostsPage(categoryID, page int) ([]wpPost, error) {
	client := &http.Client{Timeout: defaultTimeout}
	url := fmt.Sprintf(wpPostsURLFmt, categoryID, page)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 400 {
		// WP risponde 400 quando si chiede una pagina oltre l'ultima.
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var posts []wpPost
	if err := json.NewDecoder(resp.Body).Decode(&posts); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return posts, nil
}

func upsertPost(app core.App, collection *core.Collection, p *wpPost, categoryID int, categoryName string) error {
	wpID := strconv.Itoa(p.ID)

	rec, err := app.FindFirstRecordByData(collection.Id, "wp_id", wpID)
	if err != nil || rec == nil {
		rec = core.NewRecord(collection)
		rec.Set("wp_id", wpID)
	}

	rec.Set("title", cleanRendered(p.Title.Rendered, 500))
	rec.Set("excerpt", cleanRendered(p.Excerpt.Rendered, maxExcerptChars))
	rec.Set("body", cleanRendered(p.Content.Rendered, maxBodyChars))
	rec.Set("link", p.Link)
	rec.Set("image", featuredImageURL(p))
	rec.Set("category_id", strconv.Itoa(categoryID))
	if categoryName != "" {
		rec.Set("category_name", categoryName)
	}
	if t, err := time.Parse("2006-01-02T15:04:05", p.Date); err == nil {
		rec.Set("published_at", t)
	}

	return app.Save(rec)
}

func featuredImageURL(p *wpPost) string {
	if len(p.Embedded.FeaturedMedia) == 0 {
		return ""
	}
	return p.Embedded.FeaturedMedia[0].SourceURL
}

// cleanRendered rimuove tag HTML, decodifica entita`, colla la whitespace e
// tronca al limite indicato.
func cleanRendered(s string, max int) string {
	if s == "" {
		return ""
	}
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = whitespaceRE.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		s = s[:max]
	}
	return s
}
