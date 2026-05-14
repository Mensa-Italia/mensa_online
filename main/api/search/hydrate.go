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

func hydrateRecord(typ string, rec *core.Record, score float64) Item {
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
