package migrations

import (
	_ "embed"
	"errors"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

//go:embed baseline_schema.json
var baselineSchema []byte

func init() {
	m.Register(func(app core.App) error {
		return app.ImportCollectionsByMarshaledJSON(baselineSchema, false)
	}, func(app core.App) error {
		return errors.New("rollback della migrazione baseline non supportato")
	})
}
