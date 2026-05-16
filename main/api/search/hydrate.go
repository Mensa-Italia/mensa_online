package search

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

const pbFilesBase = "https://svc.mensa.it/api/files"

// Item is the hydrated or minimal search result returned to the client.
type Item struct {
	ID       string  `json:"id"`
	Score    float64 `json:"score"`
	Title    string  `json:"title,omitempty"`
	Subtitle string  `json:"subtitle,omitempty"`
	Image    string  `json:"image,omitempty"`
	DeepLink string  `json:"deep_link,omitempty"`
}

func hydrateRecord(app core.App, typ string, rec *core.Record, score float64) Item {
	item := Item{
		ID:    rec.Id,
		Score: score,
	}
	switch typ {
	case "event":
		item.Title = rec.GetString("name")
		if t := rec.GetDateTime("when_start"); !t.IsZero() {
			item.Subtitle = t.Time().Format("02 Jan 2006")
		}
		item.Image = firstFileURL(rec, "image")
		item.DeepLink = "mensa://events/" + rec.Id
	case "sig":
		item.Title = rec.GetString("name")
		item.Subtitle = rec.GetString("group_type")
		item.Image = firstFileURL(rec, "image")
		item.DeepLink = "mensa://sigs/" + rec.Id
	case "deal":
		item.Title = rec.GetString("name")
		item.Subtitle = rec.GetString("commercial_sector")
		item.Image = firstFileURL(rec, "image")
		item.DeepLink = "mensa://deals/" + rec.Id
	case "document":
		item.Title = rec.GetString("name")
		item.Subtitle = rec.GetString("category")
		item.DeepLink = "mensa://documents/" + rec.Id
	case "member":
		item.Title = rec.GetString("name")
		// Subtitle: prima la regione/stato, poi la citta` se diversa
		state := rec.GetString("state")
		city := rec.GetString("city")
		if state != "" && city != "" {
			item.Subtitle = city + ", " + state
		} else if state != "" {
			item.Subtitle = state
		} else {
			item.Subtitle = city
		}
		item.Image = firstFileURL(rec, "image")
		item.DeepLink = "mensa://members/" + rec.Id
	case "org_group":
		// rec e` un org_chart_groups. Title = nome del gruppo, deep_link
		// apre l'organigramma su quel gruppo.
		item.Title = rec.GetString("title")
		item.DeepLink = "mensa://org-chart/" + rec.Id
	case "quid_issue":
		// Numero di Quid. Due varianti distinte dal deep link:
		//   - web (categorie WP con articoli): mensa://quid/<category_id>
		//   - PDF (storico 1-12, scrappato da /archivio-quid/): mensa://quid-pdf/<id PB>
		// L'app routa direttamente dal deep link.
		item.Title = rec.GetString("name")
		item.Image = rec.GetString("image")
		if pdf := rec.GetString("pdf_url"); pdf != "" {
			item.Subtitle = "PDF"
			item.DeepLink = "mensa://quid-pdf/" + rec.Id
		} else {
			count := rec.GetInt("articles_count")
			if count == 1 {
				item.Subtitle = "1 articolo"
			} else if count > 1 {
				item.Subtitle = fmt.Sprintf("%d articoli", count)
			}
			item.DeepLink = "mensa://quid/" + rec.GetString("category_id")
		}
	case "linktree_link":
		// Link del linktree di un gruppo locale. Title = titolo del link,
		// subtitle = nome del gruppo, image = avatar del gruppo, deep_link
		// porta direttamente al linktree del gruppo via slug.
		item.Title = rec.GetString("title")
		officeName := ""
		slug := ""
		if oid := rec.GetString("local_office"); oid != "" {
			if o, err := app.FindRecordById("local_offices", oid); err == nil {
				officeName = o.GetString("name")
				slug = o.GetString("slug")
				if !o.GetDateTime("created").Time().IsZero() {
					item.Image = firstFileURL(o, "image")
				}
			}
		}
		item.Subtitle = officeName
		_ = slug // slug presente per estensione futura, non usato in deep link per ora
		item.DeepLink = "mensa://local-office/" + slug
	case "quid_article":
		// Articolo Quid in cache da WordPress. Subtitle = numero ("Quid 16 - La Fine"),
		// image = featured media. Deep link allo stile flat scelto in app.
		item.Title = rec.GetString("title")
		item.Subtitle = rec.GetString("category_name")
		item.Image = rec.GetString("image")
		item.DeepLink = "mensa://quid-article/" + rec.GetString("wp_id")
	case "podcast":
		item.Title = rec.GetString("title")
		count := rec.GetInt("episodes_count")
		if count == 1 {
			item.Subtitle = "1 episodio"
		} else if count > 1 {
			item.Subtitle = fmt.Sprintf("%d episodi", count)
		}
		item.Image = firstFileURL(rec, "image")
		item.DeepLink = "mensa://podcast/" + rec.Id
	case "podcast_episode":
		item.Title = rec.GetString("title")
		// Subtitle: durata leggibile.
		if d := rec.GetInt("duration_seconds"); d > 0 {
			m := d / 60
			s := d % 60
			item.Subtitle = fmt.Sprintf("%d:%02d", m, s)
		}
		item.Image = firstFileURL(rec, "image")
		item.DeepLink = "mensa://podcast-episode/" + rec.Id
	case "org_role":
		// rec e` un org_chart_members. Title = ruolo, Subtitle = nome socio,
		// deep_link verso il gruppo della carica.
		item.Title = rec.GetString("role")
		item.Subtitle = rec.GetString("group")
		item.DeepLink = "mensa://org-chart/" + rec.GetString("group")
	}
	return item
}

func minimalItem(id string, score float64) Item {
	return Item{ID: id, Score: score}
}

// firstFileURL returns the public URL for the first file in the named field,
// or "" if the field is empty or unset.
func firstFileURL(rec *core.Record, field string) string {
	files := rec.GetStringSlice(field)
	if len(files) == 0 || files[0] == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s", pbFilesBase, rec.BaseFilesPath(), files[0])
}
