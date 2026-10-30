package migrations

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// production_schema.json e` l'export delle collection di produzione
// (Dashboard -> Settings -> Export collections) al 30/10/2026. Sostituisce
// la vecchia baseline e le migrazioni incrementali che ci stavano sopra:
// in produzione erano gia` tutte applicate, e le righe che restano in
// _migrations per file che non esistono piu` vengono ignorate dal runner.
//
// Il 30/10 l'export e` stato riallineato a produzione dopo le modifiche
// fatte a mano dalla dashboard: view_medals (timbri validi = quelli che in
// classifica valgono almeno un punto) e le tre viste nuove della
// classifica, view_leaderboard, view_leaderboard_regioni e
// view_leaderboard_segretari, con le loro regole; le regole di stamp e
// stamp_users; e cio` che le migrazioni successive a questa creano
// (users.access_level, app_attest_keys, ex_export_requests), che restano
// idempotenti. Le query delle viste sono anche in Work/view_*.sql, fuori
// dal repo, con i commenti sul calcolo.
//
// Su un DB nuovo crea tutto lo schema; su produzione e` un no-op, perche`
// lo schema e` gia` quello. deleteMissing=false: una collection che non
// sta nell'export non viene toccata.
//
// Le modifiche successive vanno in nuove migrazioni accanto a questa.
//
// Unica differenza che resta fra un DB nuovo e produzione: gli id di
// users.name e users.avatar. Produzione e` nata con una PocketBase che li
// chiamava users_name/users_avatar, un DB nuovo li ha gia` creati con altri
// id, e l'import tiene il campo esistente (con le sue opzioni) per non
// droppare la colonna. alignFieldOptions riporta comunque le opzioni a
// quelle di produzione, lasciando l'id com'e`.
//
// Le view che esistono gia` con la stessa query e le stesse regole restano
// fuori dall'import. Salvare una view la valida eseguendone la query, e in
// produzione quella di users_notifications_region (join di user_notifications
// con members_registry, ordinata) supera il timeout di 30s delle query: il
// primo deploy di questa migrazione si e` fermato li`. Una view identica non
// ha niente da aggiornare, quindi non serve toccarla.

//go:embed production_schema.json
var productionSchema []byte

func init() {
	m.Register(func(app core.App) error {
		toImport, err := collectionsToImport(app, productionSchema)
		if err != nil {
			return err
		}
		if len(toImport) > 0 {
			if err := app.ImportCollections(toImport, false); err != nil {
				return err
			}
		}
		return alignFieldOptions(app, productionSchema)
	}, func(app core.App) error {
		return errors.New("rollback della migrazione dello schema di produzione non supportato")
	})
}

// collectionsToImport ritorna le collection di schema da importare, cioe`
// tutte tranne le view che esistono gia` identiche (stessa query e stesse
// regole).
func collectionsToImport(app core.App, schema []byte) ([]map[string]any, error) {
	var collections []map[string]any
	if err := json.Unmarshal(schema, &collections); err != nil {
		return nil, err
	}

	toImport := make([]map[string]any, 0, len(collections))
	for _, c := range collections {
		if c["type"] == core.CollectionTypeView {
			id, _ := c["id"].(string)
			existing, err := app.FindCollectionByNameOrId(id)
			if err == nil && sameView(existing, c) {
				continue
			}
			// Una vista dell'export che una migrazione successiva ha
			// trasformato in tabella (1793600000_leaderboard_tables per le tre
			// classifiche, 1793900000_medals_table per view_medals): su un
			// DB gia' migrato c'e' una collection base con quel nome, e
			// reimportare la vista la rimetterebbe al posto della tabella.
			name, _ := c["name"].(string)
			if byName, err := app.FindCollectionByNameOrId(name); err == nil && !byName.IsView() {
				continue
			}
		}
		toImport = append(toImport, c)
	}
	return orderViewsByDependency(toImport), nil
}

// orderViewsByDependency sposta ogni view dopo le view che la sua query
// nomina. L'export della dashboard e` in ordine alfabetico, e salvare una
// view la valida eseguendone la query: su un DB nuovo view_leaderboard
// (che legge view_medals e view_leaderboard_regioni) importata prima di
// view_medals si fermava con "no such table". Le collection base e auth
// restano dove sono, prima delle view che le usano.
func orderViewsByDependency(collections []map[string]any) []map[string]any {
	names := make(map[string]int, len(collections))
	for i, c := range collections {
		if c["type"] == core.CollectionTypeView {
			if name, _ := c["name"].(string); name != "" {
				names[name] = i
			}
		}
	}
	if len(names) == 0 {
		return collections
	}

	dependsOn := func(c map[string]any, name string) bool {
		query, _ := c["viewQuery"].(string)
		for _, token := range strings.FieldsFunc(query, func(r rune) bool {
			return r != '_' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
		}) {
			if token == name {
				return true
			}
		}
		return false
	}

	ordered := make([]map[string]any, 0, len(collections))
	placed := make(map[string]bool, len(names))
	var place func(c map[string]any, depth int)
	place = func(c map[string]any, depth int) {
		name, _ := c["name"].(string)
		if placed[name] {
			return
		}
		placed[name] = true
		if c["type"] == core.CollectionTypeView && depth < len(names) {
			for other, i := range names {
				if other != name && dependsOn(c, other) {
					place(collections[i], depth+1)
				}
			}
		}
		ordered = append(ordered, c)
	}
	for _, c := range collections {
		place(c, 0)
	}
	return ordered
}

func sameView(existing *core.Collection, imported map[string]any) bool {
	query, _ := imported["viewQuery"].(string)
	return existing.IsView() &&
		existing.Name == imported["name"] &&
		strings.TrimSpace(existing.ViewQuery) == strings.TrimSpace(query) &&
		sameRule(existing.ListRule, imported["listRule"]) &&
		sameRule(existing.ViewRule, imported["viewRule"]) &&
		sameRule(existing.CreateRule, imported["createRule"]) &&
		sameRule(existing.UpdateRule, imported["updateRule"]) &&
		sameRule(existing.DeleteRule, imported["deleteRule"])
}

// sameRule confronta una regola salvata (nil = solo superuser) con quella
// dello schema (null nel JSON).
func sameRule(existing *string, imported any) bool {
	rule, ok := imported.(string)
	if !ok {
		return existing == nil
	}
	return existing != nil && *existing == rule
}

// alignFieldOptions applica le opzioni dei campi di schema ai campi con lo
// stesso nome e tipo ma id diverso, che l'import ha lasciato come erano. Le
// view sono escluse: i loro campi li ricava PocketBase dalla query, e i loro
// id (_clone_*) cambiano a ogni salvataggio.
func alignFieldOptions(app core.App, schema []byte) error {
	var collections []struct {
		ID     string            `json:"id"`
		Type   string            `json:"type"`
		Fields []json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(schema, &collections); err != nil {
		return err
	}

	for _, c := range collections {
		if c.Type == core.CollectionTypeView {
			continue
		}

		collection, err := app.FindCollectionByNameOrId(c.ID)
		if err != nil {
			return err
		}

		changed := false
		for _, raw := range c.Fields {
			var imported struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &imported); err != nil {
				return err
			}

			field := collection.Fields.GetByName(imported.Name)
			if field == nil || field.GetId() == imported.ID || field.Type() != imported.Type {
				continue
			}

			id := field.GetId()
			if err := json.Unmarshal(raw, field); err != nil {
				return err
			}
			field.SetId(id)
			changed = true
		}

		if changed {
			if err := app.Save(collection); err != nil {
				return err
			}
		}
	}

	return nil
}
