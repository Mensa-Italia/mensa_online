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
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	MembersTable     = "view_leaderboard"
	RegionsTable     = "view_leaderboard_regioni"
	SecretariesTable = "view_leaderboard_segretari"
	HistoryTable     = "leaderboard_history"
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

// Refresh ricalcola le tre classifiche e riscrive le tabelle in una sola
// transazione: chi legge vede sempre o la classifica di prima o quella
// nuova, mai una a meta'. Le righe esistenti si aggiornano per id, quelle
// sparite (un socio uscito, una sede senza punti) si cancellano.
func Refresh(app core.App) error {
	started := time.Now()
	return app.RunInTransaction(func(tx core.App) error {
		for _, board := range Boards {
			if err := refreshBoard(tx, board); err != nil {
				return fmt.Errorf("%s: %w", board.Table, err)
			}
		}
		app.Logger().Info("[leaderboard] classifiche ricalcolate", "in", time.Since(started).String())
		return nil
	})
}

func refreshBoard(tx core.App, board Board) error {
	collection, err := tx.FindCollectionByNameOrId(board.Table)
	if err != nil {
		return err
	}

	var rows []dbx.NullStringMap
	if err := tx.DB().NewQuery(board.sql).All(&rows); err != nil {
		return fmt.Errorf("query: %w", err)
	}

	previous, err := lastSnapshot(tx, board.Key)
	if err != nil {
		return err
	}

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
// punti dell'ultimo snapshot salvato (ieri sera, normalmente).
func lastSnapshot(tx core.App, board string) (map[string]snapshot, error) {
	var rows []struct {
		Subject  string  `db:"subject"`
		Position float64 `db:"position"`
		Score    float64 `db:"score"`
	}
	err := tx.DB().NewQuery(`
		SELECT h.subject, h.position, h.score
		FROM ` + HistoryTable + ` h
		WHERE h.board = {:board}
		  AND h.day = (SELECT MAX(day) FROM ` + HistoryTable + ` WHERE board = {:board})
	`).Bind(dbx.Params{"board": board}).All(&rows)
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
// tre classifiche. Va chiamato a fine giornata: il giorno e' la chiave, e
// una seconda chiamata nello stesso giorno sovrascrive la prima.
func Snapshot(app core.App) error {
	day := time.Now().Format("2006-01-02")
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
