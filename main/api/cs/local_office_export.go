package cs

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/xuri/excelize/v2"

	"mensadb/tools/dbtools"
	"mensadb/tools/memberchanges"
	"mensadb/tools/regionkey"
	"mensadb/tools/season"
)

// xlsxContentType e' il tipo MIME di un file Excel.
const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// exportMovement e' la colonna Movimento del file.
var exportMovement = map[string]string{
	memberchanges.Joined:   "Nuovo socio",
	memberchanges.MovedIn:  "Arrivato da altra regione",
	memberchanges.Left:     "Non rinnovato",
	memberchanges.MovedOut: "Trasferito in altra regione",
	"active":               "Attivo",
}

// exportRow e' una riga del file.
type exportRow struct {
	ID    string `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
	City  string `db:"city"`
	State string `db:"state"`
	Kind  string `db:"kind"`
	Day   string `db:"day"`
}

// LocalOfficeMembersExportHandler e'
// GET /api/cs/local-office/{id}/members-export?kind=new|left|active&from=&to=:
// l'elenco dei soci di una sede in un file .xlsx (foglio "Soci"), per
// l'area segreteria dell'app.
//
//   - kind=new (predefinito): i movimenti joined e moved_in della sede;
//   - kind=left: i movimenti left e moved_out; agli admin della sede solo
//     quelli degli ultimi memberchanges.ExitsWindowDays giorni, qualunque
//     from chiedano;
//   - kind=active: i soci attivi della regione, per nome (from e to non
//     contano).
//
// from e to sono giorni "AAAA-MM-GG" (ora di Roma), compresi; senza, gli
// ultimi 30 giorni. La mail e' l'alias @mensa.it del socio (alias_mail),
// mai la mail originale.
//
// Solo gli admin della sede (local_offices_admins) e gli amministratori
// dell'app (powers super); 403 per gli altri.
func LocalOfficeMembersExportHandler(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("Autenticazione richiesta", nil)
	}
	officeID := e.Request.PathValue("id")
	if !canExportOffice(e.App, e.Auth, officeID) {
		return e.ForbiddenError("Solo gli admin della sede possono esportare i soci", nil)
	}
	office, err := e.App.FindRecordById("local_offices", officeID)
	if err != nil {
		return e.NotFoundError("Sede non trovata", err)
	}

	q := e.Request.URL.Query()
	kind := q.Get("kind")
	if kind == "" {
		kind = "new"
	}
	if kind != "new" && kind != "left" && kind != "active" {
		return e.BadRequestError("kind deve essere new, left o active", nil)
	}

	now := time.Now()
	var rows []exportRow
	if kind == "active" {
		rows, err = exportActive(e.App, memberchanges.OfficeKey(office))
	} else {
		from, to, perr := exportPeriod(q.Get("from"), q.Get("to"), now)
		if perr != nil {
			return e.BadRequestError(perr.Error(), nil)
		}
		kinds := []any{memberchanges.Joined, memberchanges.MovedIn}
		if kind == "left" {
			kinds = []any{memberchanges.Left, memberchanges.MovedOut}
			if !isSuperExporter(e.Auth) {
				from, to = clampExitsPeriod(from, to, now)
			}
		}
		rows, err = exportChanges(e.App, officeID, kinds, from, to)
	}
	if err != nil {
		return e.InternalServerError("Esportazione non riuscita", err)
	}

	content, err := buildMembersXLSX(rows)
	if err != nil {
		return e.InternalServerError("Creazione del file non riuscita", err)
	}

	slug := office.GetString("slug")
	if slug == "" {
		slug = office.Id
	}
	filename := fmt.Sprintf("soci-%s-%s-%s.xlsx", slug, kind, season.Local(now).Format("20060102"))
	e.Response.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return e.Blob(http.StatusOK, xlsxContentType, content)
}

// canExportOffice dice se auth puo' esportare i soci della sede: un
// superuser di PocketBase, un utente con powers super o un admin della sede.
func canExportOffice(app core.App, auth *core.Record, officeID string) bool {
	if auth.IsSuperuser() {
		return true
	}
	if auth.Collection().Name != "users" {
		return false
	}
	if dbtools.HasPower(auth, "super") {
		return true
	}
	if officeID == "" {
		return false
	}
	admin, err := app.FindFirstRecordByFilter("local_offices_admins", "local_office = {:o} && user = {:u}",
		dbx.Params{"o": officeID, "u": auth.Id})
	return err == nil && admin != nil
}

// isSuperExporter dice se auth vede tutte le uscite: un superuser di
// PocketBase o un utente con powers super.
func isSuperExporter(auth *core.Record) bool {
	return auth.IsSuperuser() || (auth.Collection().Name == "users" && dbtools.HasPower(auth, "super"))
}

// clampExitsPeriod restringe [from, to) agli ultimi
// memberchanges.ExitsWindowDays giorni (oggi compreso): le uscite che gli
// admin della sede possono esportare. Un periodo tutto prima resta vuoto.
func clampExitsPeriod(from, to, now time.Time) (time.Time, time.Time) {
	first := memberchanges.Day(now).AddDate(0, 0, -(memberchanges.ExitsWindowDays - 1))
	if from.Before(first) {
		from = first
	}
	if to.Before(from) {
		to = from
	}
	return from, to
}

// exportPeriod ritorna gli istanti [from, to) dei giorni from e to (ora di
// Roma, compresi). Senza, gli ultimi 30 giorni, oggi compreso.
func exportPeriod(fromParam, toParam string, now time.Time) (time.Time, time.Time, error) {
	today := memberchanges.Day(now)
	from, to := today.AddDate(0, 0, -29), today
	parse := func(s string) (time.Time, error) {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, fmt.Errorf("data non valida %q, serve AAAA-MM-GG", s)
		}
		return memberchanges.Day(d.Add(12 * time.Hour)), nil
	}
	var err error
	if fromParam != "" {
		if from, err = parse(fromParam); err != nil {
			return from, to, err
		}
	}
	if toParam != "" {
		if to, err = parse(toParam); err != nil {
			return from, to, err
		}
	}
	if to.Before(from) {
		return from, to, fmt.Errorf("to viene prima di from")
	}
	return from, to.AddDate(0, 0, 1), nil
}

func exportChanges(app core.App, officeID string, kinds []any, from, to time.Time) ([]exportRow, error) {
	fromValue, err := types.ParseDateTime(from)
	if err != nil {
		return nil, err
	}
	toValue, err := types.ParseDateTime(to)
	if err != nil {
		return nil, err
	}
	var rows []exportRow
	err = app.DB().Select(
		"c.member AS id",
		"COALESCE(NULLIF(mr.name, ''), c.name, '') AS name",
		"COALESCE(mr.alias_mail, '') AS email",
		"COALESCE(mr.city, '') AS city",
		"COALESCE(mr.state, '') AS state",
		"c.kind AS kind",
		"c.day AS day",
	).
		From(memberchanges.Table+" c").
		LeftJoin("members_registry mr", dbx.NewExp("mr.id = c.member")).
		Where(dbx.HashExp{"c.local_office": officeID}).
		AndWhere(dbx.In("c.kind", kinds...)).
		AndWhere(dbx.NewExp("c.day >= {:from} AND c.day < {:to}", dbx.Params{"from": fromValue.String(), "to": toValue.String()})).
		OrderBy("c.day DESC", "name ASC").
		All(&rows)
	return rows, err
}

func exportActive(app core.App, key string) ([]exportRow, error) {
	if key == "" {
		return nil, nil
	}
	var rows []exportRow
	err := app.DB().NewQuery(`
		SELECT mr.id AS id, COALESCE(mr.name, '') AS name, COALESCE(mr.alias_mail, '') AS email,
		  COALESCE(mr.city, '') AS city, COALESCE(mr.state, '') AS state,
		  'active' AS kind, '' AS day
		FROM members_registry mr
		WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
		  AND ` + regionkey.RegistryAreaSQL("mr.area") + ` = {:key}
		ORDER BY mr.name COLLATE NOCASE, mr.id
	`).Bind(dbx.Params{"key": key}).All(&rows)
	return rows, err
}

// buildMembersXLSX scrive il file: foglio "Soci", intestazione in grassetto.
func buildMembersXLSX(rows []exportRow) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	const sheet = "Soci"
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, err
	}
	header := []any{"Tessera", "Nome", "Email", "Citta'", "Provincia", "Movimento", "Data"}
	if err := f.SetSheetRow(sheet, "A1", &header); err != nil {
		return nil, err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, err
	}
	if err := f.SetRowStyle(sheet, 1, 1, bold); err != nil {
		return nil, err
	}
	for i, r := range rows {
		values := []any{r.ID, r.Name, r.Email, r.City, r.State, exportMovement[r.Kind], exportDay(r.Day)}
		cell, err := excelize.CoordinatesToCellName(1, i+2)
		if err != nil {
			return nil, err
		}
		if err := f.SetSheetRow(sheet, cell, &values); err != nil {
			return nil, err
		}
	}
	widths := map[string]float64{"A": 10, "B": 32, "C": 34, "D": 22, "E": 12, "F": 28, "G": 12}
	for col, w := range widths {
		if err := f.SetColWidth(sheet, col, col, w); err != nil {
			return nil, err
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// exportDay e' il giorno di un movimento come GG/MM/AAAA (ora di Roma),
// vuoto per i soci attivi.
func exportDay(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	d, err := types.ParseDateTime(raw)
	if err != nil || d.IsZero() {
		return ""
	}
	return season.Local(d.Time()).Format("02/01/2006")
}
