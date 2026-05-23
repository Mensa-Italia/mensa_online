package migrations

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Due fix per local_offices:
//
//  1. Backfill slug per uffici creati senza (es. quelli auto-creati da
//     localofficesync prima dell'introduzione del campo). Genera lo slug da
//     `region` con normalizzazione standard.
//  2. updateRule che permette ai soli admin (segretari + co-segretari) del
//     gruppo di modificare ESCLUSIVAMENTE il campo `bio`. Tutti gli altri
//     campi (name, region, slug, image) restano superuser-only via la
//     clausola `@request.body.<field>:isset = false`.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}

		// 1. Backfill slug
		recs, err := app.FindAllRecords("local_offices")
		if err != nil {
			return err
		}
		for _, r := range recs {
			if r.GetString("slug") != "" {
				continue
			}
			slug := slugifyRegion(r.GetString("region"))
			if slug == "" {
				slug = slugifyRegion(r.GetString("name"))
			}
			if slug == "" {
				continue
			}
			r.Set("slug", slug)
			if err := app.Save(r); err != nil {
				return err
			}
		}

		// 2. updateRule: solo admin + solo bio.
		rule := strings.Join([]string{
			"(@request.auth.id ?= local_offices_admins_via_local_office.user.id)",
			"@request.body.name:isset = false",
			"@request.body.region:isset = false",
			"@request.body.slug:isset = false",
			"@request.body.image:isset = false",
		}, " && ")
		col.UpdateRule = &rule
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return nil
		}
		col.UpdateRule = nil
		return app.Save(col)
	})
}

// slugifyRegion: stessa logica di tools/localofficesync.slugifyRegion ma
// duplicata qui per evitare import ciclico (le migrations sono caricate
// prima di tools/).
func slugifyRegion(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := strings.NewReplacer(
		"à", "a", "á", "a", "â", "a", "ä", "a",
		"è", "e", "é", "e", "ê", "e", "ë", "e",
		"ì", "i", "í", "i", "î", "i", "ï", "i",
		"ò", "o", "ó", "o", "ô", "o", "ö", "o",
		"ù", "u", "ú", "u", "û", "u", "ü", "u",
		"'", "", "`", "",
	)
	s = repl.Replace(s)
	out := make([]byte, 0, len(s))
	prevDash := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
			prevDash = false
		case c == ' ' || c == '-' || c == '_':
			if !prevDash {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}
