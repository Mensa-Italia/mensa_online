// Package season e' la stagione di classifiche e medaglie: dal 1 ottobre al
// 30 settembre, ora di Roma. Una stagione e' un intero, l'anno in cui
// comincia (la 2026 va dal 1/10/2026 al 30/9/2027) e si scrive "2026/27".
//
// E' la regola unica che usano le classifiche (tools/leaderboard), le
// medaglie personali (tools/medals) e quelle di regione (tools/regionmedals),
// e l'app ne ha una copia identica: il confine e' la mezzanotte di Roma, non
// quella UTC, quindi il 30 settembre alle 23:30 UTC e' gia' la stagione
// nuova. Le finestre per le query escono come istanti UTC nel formato con
// cui PocketBase salva le date, cosi' si confrontano come testo.
package season

import (
	"fmt"
	"time"
	_ "time/tzdata" // il fuso di Roma anche in un'immagine senza tzdata

	"github.com/pocketbase/dbx"
)

// StartMonth e' il mese in cui comincia la stagione.
const StartMonth = time.October

// sqlLayout e' il formato dei parametri {:start} e {:end} delle query.
// PocketBase salva le date come "2026-10-01 00:00:00.000Z": un testo piu'
// lungo con lo stesso prefisso e' maggiore, quindi `created >= {:start}` e
// `created < {:end}` danno la finestra semiaperta giusta.
const sqlLayout = "2006-01-02 15:04:05"

var rome = loadRome()

func loadRome() *time.Location {
	if loc, err := time.LoadLocation("Europe/Rome"); err == nil {
		return loc
	}
	return time.UTC
}

// Of ritorna la stagione a cui appartiene un istante.
func Of(t time.Time) int {
	local := t.In(rome)
	if local.Month() >= StartMonth {
		return local.Year()
	}
	return local.Year() - 1
}

// Current e' la stagione in corso a quest'ora.
func Current(now time.Time) int {
	return Of(now)
}

// Bounds ritorna gli istanti (UTC) di inizio e fine della stagione s:
// la mezzanotte di Roma del 1 ottobre di s e quella del 1 ottobre dopo.
// L'inizio e' compreso, la fine no.
func Bounds(s int) (start, end time.Time) {
	start = time.Date(s, StartMonth, 1, 0, 0, 0, 0, rome).UTC()
	end = time.Date(s+1, StartMonth, 1, 0, 0, 0, 0, rome).UTC()
	return start, end
}

// Label e' il nome della stagione come lo scrive l'app: "2026/27".
func Label(s int) string {
	return fmt.Sprintf("%d/%02d", s, (s+1)%100)
}

// Window ritorna i confini della stagione come testo "AAAA-MM-GG hh:mm:ss"
// in UTC, pronti per i parametri delle query.
func Window(s int) (start, end string) {
	a, b := Bounds(s)
	return a.Format(sqlLayout), b.Format(sqlLayout)
}

// Params sono i parametri {:start} e {:end} della stagione s.
func Params(s int) dbx.Params {
	start, end := Window(s)
	return dbx.Params{"start": start, "end": end}
}

// LastDay e' l'ultimo giorno (ora di Roma) della stagione s, "AAAA-09-30":
// la chiave dello snapshot finale di leaderboard_history.
func LastDay(s int) string {
	return fmt.Sprintf("%d-09-30", s+1)
}

// FirstDay e' il primo giorno (ora di Roma) della stagione s, "AAAA-10-01".
func FirstDay(s int) string {
	return fmt.Sprintf("%d-10-01", s)
}

// Local ritorna l'istante nel fuso di Roma, quello con cui si contano i
// giorni.
func Local(t time.Time) time.Time {
	return t.In(rome)
}

// Day e' il giorno di Roma di un istante, "AAAA-MM-GG": la chiave di
// leaderboard_history.
func Day(t time.Time) string {
	return t.In(rome).Format("2006-01-02")
}

// LastHour dice se a quest'ora a Roma manca meno di un'ora a mezzanotte: lo
// snapshot di fine giornata gira allora, cosi' quello del 30 settembre
// fotografa la classifica prima che cominci la stagione nuova, anche se il
// server conta le ore in UTC.
func LastHour(t time.Time) bool {
	return t.In(rome).Hour() == 23
}
