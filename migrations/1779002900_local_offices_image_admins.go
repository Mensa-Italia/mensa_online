package migrations

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Due cose su local_offices.image:
//
//  1. Bump del max size del field a 2 GB (2_000_000_000 byte). Il default
//     PB e` 5 MB e su immagini editoriali ad alta risoluzione non basta.
//     I limiti reali a quel punto sono il body size del reverse proxy e
//     lo storage S3, non PB.
//  2. updateRule allargata: i segretari / co-segretari del gruppo possono
//     ora modificare anche `image` (oltre a `bio`). Restano riservati a
//     superuser i campi identitari (name, region, slug).
//
// Idempotente: il field viene ricreato solo se manca; la rule viene
// riscritta a ogni up.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return err
		}

		// 1. Image: MaxSize 2GB + thumb 0x500 (resize a 500px di altezza,
		//    width preservato dall'aspect ratio). Serve all'app per le card
		//    senza scaricare la full-res.
		if f := col.Fields.GetByName("image"); f != nil {
			if img, ok := f.(*core.FileField); ok {
				img.MaxSize = 2_000_000_000
				if !containsThumb(img.Thumbs, "0x500") {
					img.Thumbs = append(img.Thumbs, "0x500")
				}
			}
		}

		// 2. UpdateRule: bio + image consentiti agli admin.
		rule := strings.Join([]string{
			"(@request.auth.id ?= local_offices_admins_via_local_office.user.id)",
			"@request.body.name:isset = false",
			"@request.body.region:isset = false",
			"@request.body.slug:isset = false",
		}, " && ")
		col.UpdateRule = &rule

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("local_offices")
		if err != nil {
			return nil
		}
		if f := col.Fields.GetByName("image"); f != nil {
			if img, ok := f.(*core.FileField); ok {
				img.MaxSize = 0 // torna al default PB
				img.Thumbs = removeThumb(img.Thumbs, "0x500")
			}
		}
		// Down: ripristina la rule di 1779002200 (bio-only).
		rule := strings.Join([]string{
			"(@request.auth.id ?= local_offices_admins_via_local_office.user.id)",
			"@request.body.name:isset = false",
			"@request.body.region:isset = false",
			"@request.body.slug:isset = false",
			"@request.body.image:isset = false",
		}, " && ")
		col.UpdateRule = &rule
		return app.Save(col)
	})
}

func containsThumb(list []string, t string) bool {
	for _, x := range list {
		if x == t {
			return true
		}
	}
	return false
}

func removeThumb(list []string, t string) []string {
	out := list[:0]
	for _, x := range list {
		if x != t {
			out = append(out, x)
		}
	}
	return out
}
