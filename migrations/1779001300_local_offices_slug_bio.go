package migrations

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Aggiunge a local_offices i campi necessari per la "linktree" pubblica:
//   - slug: handle URL-safe, unico per gruppo (es. "lombardia", "val-daosta")
//   - bio:  descrizione breve mostrata in cima alla pagina link
//
// Per le righe esistenti, slug viene auto-popolato da region (lowercase +
// dashed). Resta editabile a mano dagli admin.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}
		col.Fields.Add(&core.TextField{
			Name:    "slug",
			Max:     80,
			Pattern: `^[a-z0-9]+(?:-[a-z0-9]+)*$`,
		})
		col.Fields.Add(&core.TextField{Name: "bio", Max: 500})
		col.AddIndex("idx_local_offices_slug", true, "slug", "slug != ''")
		if err := app.Save(col); err != nil {
			return err
		}

		// Auto-populate slug per le righe esistenti.
		records, err := app.FindAllRecords("local_offices")
		if err != nil {
			return err
		}
		for _, rec := range records {
			if rec.GetString("slug") != "" {
				continue
			}
			rec.Set("slug", slugify(rec.GetString("region")))
			if err := app.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("slug")
		col.Fields.RemoveByName("bio")
		return app.Save(col)
	})
}

// slugify normalizza una stringa in handle URL-safe: lowercase, sostituisce
// gli spazi con dashes, rimuove apostrofi e caratteri non alfanumerici.
//   "Val d'Aosta" -> "val-daosta"
//   "Emilia Romagna" -> "emilia-romagna"
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ', r == '_', r == '-':
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			// apostrofi, accenti normalizzati al minimo, scartati per il resto
			switch r {
			case 'à', 'á', 'â', 'ã', 'ä':
				b.WriteRune('a')
			case 'è', 'é', 'ê', 'ë':
				b.WriteRune('e')
			case 'ì', 'í', 'î', 'ï':
				b.WriteRune('i')
			case 'ò', 'ó', 'ô', 'õ', 'ö':
				b.WriteRune('o')
			case 'ù', 'ú', 'û', 'ü':
				b.WriteRune('u')
			}
			prevDash = false
		}
	}
	out := b.String()
	return strings.Trim(out, "-")
}
