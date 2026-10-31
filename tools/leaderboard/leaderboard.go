// Package leaderboard tiene le tre classifiche (soci, regioni, segretari)
// come tabelle vere invece che come viste.
//
// Le viste ricalcolavano tutto a ogni richiesta: tre finestre su
// stamp_users x stamp x events, view_medals dentro, 0,6-1,9 s per lettura
// su un SQLite solo, e un costo che cresceva con i soci che aprivano l'app.
// Qui le stesse query (sql/*.sql, identiche a quelle che erano nelle viste)
// girano da un cron ogni quarto d'ora e il risultato finisce nelle
// collection base view_leaderboard, view_leaderboard_regioni e
// view_leaderboard_segretari, con gli stessi nomi, campi, id e regole di
// prima: l'app non cambia. Una lettura e' una SELECT su qualche centinaio
// di righe gia' pronte.
//
// Stagione: le classifiche contano la stagione in corso, dal 1 ottobre al 30
// settembre ora di Roma (tools/season). Le query prendono i parametri start e
// end, nessun decadimento: dentro la stagione vale pieno, fuori zero. In
// classifica dei soci compare solo chi ha almeno un timbro valido nella
// stagione, e i punti delle medaglie vengono solo dalle stagionali. Ogni riga
// porta il campo season. Alla chiusura di una stagione Finalize ne scrive
// l'ordine finale in leaderboard_seasons, l'albo d'oro.
//
// Storico: leaderboard_history prende una riga per soggetto a ogni
// snapshot giornaliero (posizione e punti di fine giornata), e ogni
// ricalcolo copia nelle righe vive previous_position e previous_score
// dall'ultimo snapshot, cosi' l'app mostra la freccia su o giu' rispetto a
// ieri senza leggere lo storico.
package leaderboard

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"mensadb/tools/medals"
	"mensadb/tools/season"
)

const (
	MembersTable     = "view_leaderboard"
	RegionsTable     = "view_leaderboard_regioni"
	SecretariesTable = "view_leaderboard_segretari"
	HistoryTable     = "leaderboard_history"
	// SeasonsTable e' l'albo d'oro: l'ordine finale di ogni stagione chiusa.
	SeasonsTable = "leaderboard_seasons"
)

//go:embed sql/regioni.sql
var regionsSQL string

//go:embed sql/segretari.sql
var secretariesSQL string

//go:embed sql/soci.sql
var membersSQL string

// Board e' una classifica: la tabella che la tiene, la query che la
// calcola e il nome con cui lo storico la chiama.
type Board struct {
	Table string
	Key   string
	sql   string
}

// Boards nell'ordine in cui vanno ricalcolate: la query dei soci legge la
// tabella delle regioni (fattore comunita'), quella dei segretari pure.
var Boards = []Board{
	{Table: RegionsTable, Key: "regions", sql: regionsSQL},
	{Table: SecretariesTable, Key: "secretaries", sql: secretariesSQL},
	{Table: MembersTable, Key: "members", sql: membersSQL},
}

// Colonne che restano testo (o relation): tutte le altre sono numeri.
var textColumns = map[string]bool{
	"id": true, "name": true, "region": true, "region_key": true, "local_office": true,
}

// Un ricalcolo alla volta: il cron e l'hook dell'opt-out possono
// sovrapporsi, e due transazioni che riscrivono le stesse righe si
// bloccherebbero a vicenda.
var refreshMu sync.Mutex

// Refresh ricalcola le tre classifiche per la stagione in corso e riscrive le
// tabelle in una sola transazione: chi legge vede sempre o la classifica di
// prima o quella nuova, mai una a meta'. Le righe esistenti si aggiornano per
// id, quelle sparite (un socio uscito, una sede senza punti, uno senza
// timbri nella stagione) si cancellano.
func Refresh(app core.App) error {
	return RefreshAt(app, time.Now())
}

// RefreshAt e' Refresh con l'orologio passato da fuori: la stagione e' quella
// di now.
func RefreshAt(app core.App, now time.Time) error {
	if err := refresh(app, now); err != nil {
		return err
	}
	if AfterRefresh != nil {
		AfterRefresh(app)
	}
	return nil
}

// RecomputeAt ricalcola come RefreshAt ma senza AfterRefresh, cioe' senza
// notifiche: serve ai riempimenti di una migrazione.
func RecomputeAt(app core.App, now time.Time) error {
	return refresh(app, now)
}

// AfterRefresh, se impostato, viene chiamato dopo ogni ricalcolo riuscito,
// a transazione chiusa e fuori dal mutex: ci si agganciano le notifiche di
// soglia (tools/leaderboardnotify), che leggono le tabelle appena
// riscritte. Lo imposta main/crons all'avvio.
var AfterRefresh func(app core.App)

func refresh(app core.App, now time.Time) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	started := time.Now()
	current := season.Current(now)
	return app.RunInTransaction(func(tx core.App) error {
		for _, board := range Boards {
			if err := refreshBoard(tx, board, current); err != nil {
				return fmt.Errorf("%s: %w", board.Table, err)
			}
		}
		app.Logger().Info("[leaderboard] classifiche ricalcolate", "season", season.Label(current), "in", time.Since(started).String())
		return nil
	})
}

func refreshBoard(tx core.App, board Board, current int) error {
	collection, err := tx.FindCollectionByNameOrId(board.Table)
	if err != nil {
		return err
	}

	var rows []dbx.NullStringMap
	if err := tx.DB().NewQuery(board.sql).Bind(season.Params(current)).All(&rows); err != nil {
		return fmt.Errorf("query: %w", err)
	}

	previous, err := lastSnapshot(tx, board.Key, season.FirstDay(current))
	if err != nil {
		return err
	}
	hasSeason := collection.Fields.GetByName("season") != nil

	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		id := row["id"].String
		if id == "" {
			continue
		}
		seen[id] = true
		record, err := tx.FindRecordById(collection, id)
		if err != nil {
			record = core.NewRecord(collection)
			record.Id = id
		}
		for column, value := range row {
			if column == "id" {
				continue
			}
			if collection.Fields.GetByName(column) == nil {
				continue
			}
			record.Set(column, cellValue(column, value))
		}
		if hasSeason {
			record.Set("season", current)
		}
		if snap, ok := previous[id]; ok {
			record.Set("previous_position", snap.position)
			record.Set("previous_score", snap.score)
		} else {
			record.Set("previous_position", 0)
			record.Set("previous_score", 0)
		}
		if err := tx.SaveNoValidate(record); err != nil {
			return fmt.Errorf("save %s: %w", id, err)
		}
	}

	existing, err := tx.FindAllRecords(collection)
	if err != nil {
		return err
	}
	for _, record := range existing {
		if !seen[record.Id] {
			if err := tx.Delete(record); err != nil {
				return fmt.Errorf("delete %s: %w", record.Id, err)
			}
		}
	}
	return nil
}

func cellValue(column string, value sql.NullString) any {
	if !value.Valid {
		if textColumns[column] {
			return ""
		}
		return 0
	}
	if textColumns[column] {
		return value.String
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(value.String), 64)
	if err != nil {
		return 0
	}
	return n
}

type snapshot struct {
	position float64
	score    float64
}

// lastSnapshot ritorna, per ogni soggetto della classifica, posizione e
// punti dell'ultimo snapshot salvato (ieri sera, normalmente), purche' sia
// della stagione in corso (day >= since): a ottobre la freccia non confronta
// con la classifica della stagione chiusa.
func lastSnapshot(tx core.App, board, since string) (map[string]snapshot, error) {
	var rows []struct {
		Subject  string  `db:"subject"`
		Position float64 `db:"position"`
		Score    float64 `db:"score"`
	}
	err := tx.DB().NewQuery(`
		SELECT h.subject, h.position, h.score
		FROM ` + HistoryTable + ` h
		WHERE h.board = {:board}
		  AND h.day = (SELECT MAX(day) FROM ` + HistoryTable + ` WHERE board = {:board} AND day >= {:since})
	`).Bind(dbx.Params{"board": board, "since": since}).All(&rows)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	out := make(map[string]snapshot, len(rows))
	for _, r := range rows {
		out[r.Subject] = snapshot{position: r.Position, score: r.Score}
	}
	return out, nil
}

// Snapshot salva nello storico posizione e punti di oggi per ogni riga delle
// tre classifiche. Va chiamato a fine giornata: il giorno (di Roma) e' la
// chiave, e una seconda chiamata nello stesso giorno sovrascrive la prima.
// Quello del 30 settembre e' lo snapshot finale della stagione.
func Snapshot(app core.App) error {
	return SnapshotAt(app, time.Now())
}

// SnapshotAt e' Snapshot con l'orologio passato da fuori.
func SnapshotAt(app core.App, now time.Time) error {
	day := season.Day(now)
	return app.RunInTransaction(func(tx core.App) error {
		history, err := tx.FindCollectionByNameOrId(HistoryTable)
		if err != nil {
			return err
		}
		for _, board := range Boards {
			rows, err := tx.FindAllRecords(board.Table)
			if err != nil {
				return err
			}
			for _, row := range rows {
				record, err := tx.FindFirstRecordByFilter(history,
					"board = {:board} && subject = {:subject} && day = {:day}",
					dbx.Params{"board": board.Key, "subject": row.Id, "day": day})
				if err != nil {
					record = core.NewRecord(history)
					record.Set("board", board.Key)
					record.Set("subject", row.Id)
					record.Set("day", day)
				}
				record.Set("name", row.GetString("name"))
				record.Set("position", row.GetFloat("position"))
				record.Set("score", row.GetFloat("score"))
				if err := tx.SaveNoValidate(record); err != nil {
					return fmt.Errorf("history %s/%s: %w", board.Key, row.Id, err)
				}
			}
		}
		app.Logger().Info("[leaderboard] snapshot salvato", "day", day)
		return nil
	})
}

// Finalize scrive in leaderboard_seasons l'ordine finale della stagione s
// (l'albo d'oro): per ogni classifica tutte le righe che la query da' con la
// finestra di quella stagione, le regioni tutte e diciotto, i soci e i
// segretari tutti. Rifa' il calcolo da zero: le query dei segretari e dei soci
// leggono view_leaderboard_regioni e view_medals, che sono della stagione in
// corso, e per una stagione chiusa vengono sostituite, per la durata della
// transazione, da tabelle temporanee calcolate con la finestra di s. Le
// righe di quella stagione gia' presenti si sostituiscono, quindi chiamarla
// due volte da' lo stesso risultato. I soci sono quelli con la tessera
// attiva adesso.
func Finalize(app core.App, s int) error {
	refreshMu.Lock()
	defer refreshMu.Unlock()
	started := time.Now()
	counts := map[string]int{}
	err := app.RunInTransaction(func(tx core.App) error {
		params := season.Params(s)
		dropMedals, err := medals.ShadowSeason(tx, s)
		if err != nil {
			return err
		}
		defer dropMedals()
		defer func() { _, _ = tx.DB().NewQuery(`DROP TABLE IF EXISTS temp.` + RegionsTable).Execute() }()
		_, _ = tx.DB().NewQuery(`DROP TABLE IF EXISTS temp.` + RegionsTable).Execute()

		for _, board := range Boards {
			var rows []dbx.NullStringMap
			if board.Key == "regions" {
				if _, err := tx.DB().NewQuery("CREATE TEMP TABLE " + RegionsTable + " AS\n" + board.sql).Bind(params).Execute(); err != nil {
					return fmt.Errorf("%s: %w", board.Key, err)
				}
				if err := tx.DB().NewQuery("SELECT * FROM temp." + RegionsTable).All(&rows); err != nil {
					return fmt.Errorf("%s: %w", board.Key, err)
				}
			} else if err := tx.DB().NewQuery(board.sql).Bind(params).All(&rows); err != nil {
				return fmt.Errorf("%s: %w", board.Key, err)
			}
			if err := storeSeason(tx, board.Key, s, rows); err != nil {
				return fmt.Errorf("%s: %w", board.Key, err)
			}
			counts[board.Key] = len(rows)
		}
		return nil
	})
	if err != nil {
		return err
	}
	app.Logger().Info("[leaderboard] albo d'oro archiviato", "season", season.Label(s),
		"members", counts["members"], "regions", counts["regions"], "secretaries", counts["secretaries"],
		"in", time.Since(started).String())
	return nil
}

// storeSeason sostituisce le righe dell'albo di una classifica e stagione.
func storeSeason(tx core.App, board string, s int, rows []dbx.NullStringMap) error {
	if _, err := tx.DB().NewQuery(`DELETE FROM ` + SeasonsTable + ` WHERE board = {:b} AND season = {:s}`).
		Bind(dbx.Params{"b": board, "s": s}).Execute(); err != nil {
		return err
	}
	stamp := types.NowDateTime().String()
	for _, row := range rows {
		subject := row["id"].String
		if subject == "" {
			continue
		}
		params := dbx.Params{
			"id": core.GenerateDefaultRandomId(), "board": board, "season": s, "subject": subject,
			"name":       cellValue("name", row["name"]),
			"position":   cellValue("position", row["position"]),
			"score":      cellValue("score", row["score"]),
			"region_key": "", "local_office": "", "created": stamp,
		}
		switch board {
		case "regions":
			params["region_key"] = cellValue("region_key", row["region_key"])
			params["local_office"] = subject
		case "secretaries":
			params["local_office"] = cellValue("local_office", row["local_office"])
		}
		if _, err := tx.DB().Insert(SeasonsTable, params).Execute(); err != nil {
			return fmt.Errorf("%s: %w", subject, err)
		}
	}
	return nil
}

// EnsureSeasonsCollection crea leaderboard_seasons se manca. Lettura per i
// soci autenticati: i primi dieci di ogni stagione e la riga di chi legge.
// Scrittura solo superuser.
func EnsureSeasonsCollection(app core.App) error {
	if existing, _ := app.FindCollectionByNameOrId(SeasonsTable); existing != nil {
		return nil
	}
	c := core.NewBaseCollection(SeasonsTable)
	rule := `@request.auth.id != "" && (position <= 10 || subject = @request.auth.id)`
	c.ListRule = &rule
	c.ViewRule = &rule
	c.Fields.Add(
		&core.SelectField{Name: "board", Values: []string{"members", "regions", "secretaries"}, MaxSelect: 1, Required: true},
		&core.NumberField{Name: "season", OnlyInt: true, Required: true},
		// Chi e': numero tessera dei soci e dei segretari, id della sede
		// per le regioni.
		&core.TextField{Name: "subject", Required: true},
		&core.TextField{Name: "name"},
		&core.NumberField{Name: "position"},
		&core.NumberField{Name: "score"},
		// Solo per le regioni: la regione come in view_leaderboard_regioni.region_key.
		&core.TextField{Name: "region_key"},
		// Per regioni e segretari: l'id della sede.
		&core.TextField{Name: "local_office"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	c.AddIndex("idx_leaderboard_seasons_key", true, "board, season, subject", "")
	c.AddIndex("idx_leaderboard_seasons_rank", false, "board, season, position", "")
	if err := app.Save(c); err != nil {
		return fmt.Errorf("creazione %s: %w", SeasonsTable, err)
	}
	return nil
}

// RequestRefresh accoda un ricalcolo in sottofondo, con debounce: la prima
// richiesta fa partire un timer di [refreshDelay], le successive nel
// frattempo non aggiungono niente. Cosi' chi si toglie e si rimette cento
// volte di fila, per dispetto, ottiene un ricalcolo solo, e il ricalcolo
// vede comunque lo stato finale perche' legge il DB quando parte.
func RequestRefresh(app core.App) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if pending {
		return
	}
	pending = true
	time.AfterFunc(refreshDelay, func() {
		pendingMu.Lock()
		pending = false
		pendingMu.Unlock()
		if err := Refresh(app); err != nil {
			app.Logger().Error("[leaderboard] refresh richiesto", "err", err)
		}
	})
}

const refreshDelay = 10 * time.Second

var (
	pendingMu sync.Mutex
	pending   bool
)

// Hide toglie subito la riga di un socio dalla classifica dei soci: chi
// sceglie di non apparire non deve aspettare il cron. Le posizioni degli
// altri restano quelle di prima fino al ricalcolo, che l'hook fa partire
// subito dopo in sottofondo. Nessun errore se la riga non c'e'.
func Hide(app core.App, userID string) error {
	record, err := app.FindRecordById(MembersTable, userID)
	if err != nil {
		return nil
	}
	return app.Delete(record)
}
