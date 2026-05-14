package quidsync

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

const archiveURL = "https://quid.mensa.it/archivio-quid/"

// Pattern usati per estrarre i numeri storici dall'HTML di /archivio-quid/.
//   - titlePattern: <strong>Quid N: titolo</strong>  (sorgente autoritativa
//     per nome leggibile, copre 1-16).
//   - pdfPattern: link a PDF tipo .../QUID-01-LIntelligenza.pdf o Quid_07-LErrore.pdf
//   - coverPattern: jpg di copertina con prefisso Quid_NN-...
var (
	titlePattern = regexp.MustCompile(`(?i)<strong>\s*Quid\s+(\d+)\s*:\s*([^<]+?)\s*</strong>`)
	pdfPattern   = regexp.MustCompile(`(?i)https?://[^"'\s]*[Qq][Uu][Ii][Dd][-_](\d+)[^"'\s]*\.pdf`)
	coverPattern = regexp.MustCompile(`(?i)https?://[^"'\s]*[Qq][Uu][Ii][Dd][-_](\d+)[^"'\s]*-300x[^"'\s]*\.(?:jpg|jpeg|png)`)
)

type archiveEntry struct {
	Number int
	Title  string
	PDFURL string
	Cover  string
}

// fetchArchive scarica l'HTML di /archivio-quid/ e ne estrae la mappa
// numero → entry (titolo, pdf, cover). Restituisce solo i numeri che hanno
// almeno un PDF, perche` quelli senza PDF (13-16) sono gestiti via WP REST
// API come categorie con articoli.
func fetchArchive() (map[int]*archiveEntry, error) {
	client := &http.Client{Timeout: defaultTimeout}
	resp, err := client.Get(archiveURL)
	if err != nil {
		return nil, fmt.Errorf("GET archive: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET archive: status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read archive body: %w", err)
	}
	body := string(bodyBytes)

	entries := map[int]*archiveEntry{}

	for _, m := range titlePattern.FindAllStringSubmatch(body, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(m[2]))
		// Capitalizza prima lettera per consistenza coi titoli WP ("Quid 16 - La Fine").
		if title != "" {
			title = strings.ToUpper(title[:1]) + title[1:]
		}
		e, ok := entries[n]
		if !ok {
			e = &archiveEntry{Number: n}
			entries[n] = e
		}
		if e.Title == "" {
			e.Title = title
		}
	}

	for _, m := range pdfPattern.FindAllStringSubmatch(body, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		e, ok := entries[n]
		if !ok {
			e = &archiveEntry{Number: n}
			entries[n] = e
		}
		if e.PDFURL == "" {
			e.PDFURL = m[0]
		}
	}

	for _, m := range coverPattern.FindAllStringSubmatch(body, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		e, ok := entries[n]
		if !ok {
			continue
		}
		if e.Cover == "" {
			e.Cover = m[0]
		}
	}

	// Filtra: tengo solo i numeri con un PDF (sono quelli che WP REST non espone).
	out := make(map[int]*archiveEntry, len(entries))
	for n, e := range entries {
		if e.PDFURL == "" {
			continue
		}
		out[n] = e
	}
	return out, nil
}

// SyncArchive popola quid_issues con i numeri storici di Quid che esistono
// solo come PDF (1-12 al momento). Usa /archivio-quid/ come fonte. Non tocca
// i numeri gia` sincronizzati come categorie WP: l'idempotenza e` garantita
// dall'index unique su category_id ("pdf-N" per i PDF, id WP numerico per
// quelli web).
func SyncArchive(app core.App) (int, error) {
	entries, err := fetchArchive()
	if err != nil {
		return 0, err
	}
	collection, err := app.FindCollectionByNameOrId("quid_issues")
	if err != nil {
		return 0, fmt.Errorf("find collection quid_issues: %w", err)
	}

	upserted := 0
	for n, e := range entries {
		categoryID := "pdf-" + strconv.Itoa(n)
		rec, err := app.FindFirstRecordByData(collection.Id, "category_id", categoryID)
		if err != nil || rec == nil {
			rec = core.NewRecord(collection)
			rec.Set("category_id", categoryID)
		}
		rec.Set("number", n)
		// Nome leggibile: "Quid NN - Titolo". Se manca il titolo, lascia solo "Quid NN".
		name := fmt.Sprintf("Quid %02d", n)
		if e.Title != "" {
			name = fmt.Sprintf("Quid %02d - %s", n, e.Title)
		}
		rec.Set("name", name)
		rec.Set("articles_count", 0)
		rec.Set("pdf_url", e.PDFURL)
		if e.Cover != "" {
			rec.Set("image", e.Cover)
		}
		if err := app.Save(rec); err != nil {
			app.Logger().Error("[quidsync] upsert issue PDF fallito", "number", n, "err", err)
			continue
		}
		upserted++
	}
	return upserted, nil
}
