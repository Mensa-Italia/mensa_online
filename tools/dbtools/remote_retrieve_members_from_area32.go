package dbtools

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mensadb/area32"
	"mensadb/importers"
	"mensadb/tools/cdnfiles"
	"mensadb/tools/env"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
)

func RemoteRetrieveMembersFromArea32(app core.App) {
	// Recupera le credenziali dall'ambiente
	email := env.GetArea32InternalEmail()
	password := env.GetArea32InternalPassword()

	SnapshotArea32Members(app)

	// Inizializza l'API Area32 per autenticare l'utente e ottenere i dati principali
	scraperApi := area32.NewAPI()
	_, err := scraperApi.DoLoginAndRetrieveMain(email, password)

	if err != nil {
		// Se l'autenticazione fallisce, termina la funzione
		return
	}

	// Recupera i nuovi membri da Area32
	newMembers, err := scraperApi.GetAllRegSoci()
	if err != nil {
		app.Logger().Error("members sync: GetAllRegSoci failed, abort to avoid mass deactivation", "err", err)
		return
	}

	// Recupera la collezione "members_registry" dal database
	membersRegistryCollection, err := app.FindCollectionByNameOrId("members_registry")
	if err != nil {
		return
	}

	// Recupera tutti i membri presenti nel database
	membersInside, err := app.FindAllRecords(membersRegistryCollection)
	if err != nil {
		return
	}

	// Tripwire: se Area32 ha restituito una lista sospettosamente piccola rispetto
	// allo stato del DB, abortisce senza toccare nulla. Evita il caso "scrape fallito
	// a meta` e disattiva tutti tranne i pochi visti".
	if len(membersInside) > 100 && len(newMembers)*2 < len(membersInside) {
		app.Logger().Error("members sync: aborting, suspiciously few members from Area32",
			"got", len(newMembers), "db_total", len(membersInside))
		return
	}

	// Aggiorna i membri trovati su Area32
	allMembersIDs := []string{}
	for _, member := range newMembers {
		allMembersIDs = append(allMembersIDs, UpdateMembers(app, member))
	}

	// Costruisce un elenco degli ID dei membri esistenti
	membersUids := []string{}
	for _, member := range membersInside {
		membersUids = append(membersUids, member.Id)
	}

	// Costruisci un set degli ID restituiti da Area32 per lookup O(1)
	// (prima era un doppio loop O(n^2) su ~50k membri).
	existing := make(map[string]struct{}, len(allMembersIDs))
	for _, id := range allMembersIDs {
		existing[id] = struct{}{}
	}

	// per i memberi in memberUids che non sono in allMembersIDs imposto is_active a false
	for _, member := range membersUids {
		_, found := existing[member]
		if !found {
			memberInside, err := app.FindRecordById(membersRegistryCollection, member)
			if err == nil {
				memberInside.Set("is_active", false)
				if err := app.Save(memberInside); err != nil {
					app.Logger().Error("members sync: save failed", "id", memberInside.Id, "err", err)
				}
			}
			userInside, err := app.FindRecordById("users", member)
			if err == nil {
				userInside.Set("is_membership_active", false)
				if err := app.Save(userInside); err != nil {
					app.Logger().Error("members sync: save failed", "id", userInside.Id, "err", err)
				}
			}
		} else {
			userInside, err := app.FindRecordById("users", member)
			if err == nil && !userInside.GetBool("is_membership_active") {
				userInside.Set("is_membership_active", true)
				if err := app.Save(userInside); err != nil {
					app.Logger().Error("members sync: save failed", "id", userInside.Id, "err", err)
				}
			}
		}
	}

}

// memberDataDigest racchiude in ordine deterministico i campi anagrafici
// da hashare per decidere se una riga members_registry e` cambiata.
// json.Marshal di una struct rispetta l'ordine dei campi, quindi l'output
// e` stabile e adatto a un confronto SHA256.
type memberDataDigest struct {
	Name            string `json:"name"`
	City            string `json:"city"`
	Birthdate       string `json:"birthdate"`
	State           string `json:"state"`
	Area            string `json:"area"`
	OriginalMail    string `json:"original_mail"`
	AliasMail       string `json:"alias_mail"`
	FullData        string `json:"full_data"`
	IsActive        bool   `json:"is_active"`
	FullProfileLink string `json:"full_profile_link"`
}

func computeDataHash(d memberDataDigest) string {
	buf, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(buf)
	return hex.EncodeToString(h[:])
}

// Funzione per aggiornare i membri nel database
func UpdateMembers(app core.App, member map[string]any) string {
	id, err := app.FindCollectionByNameOrId("members_registry")
	if err != nil {
		return ""
	}
	memberId := member["uid"].(string)

	newRecord, err := app.FindRecordById(id, memberId)
	isNew := false
	if err != nil {
		newRecord = core.NewRecord(id)
		newRecord.Id = memberId
		isNew = true
	}

	name, _ := member["name"].(string)
	city, _ := member["city"].(string)
	state, _ := member["state"].(string)
	area, _ := member["area"].(string)
	fullProfileLink, _ := member["full_profile_link"].(string)

	// birthdate puo` essere time.Time o stringa: serializziamo a stringa per
	// avere un input deterministico al digest.
	birthdate := member["birthDate"]
	birthdateStr := ""
	if b, err := json.Marshal(birthdate); err == nil {
		birthdateStr = string(b)
	}

	// Estrae email + alias e ricostruisce il payload full_data (mantenendo
	// la stessa logica originale di sostituzione mailto: con l'alias).
	originalMail, aliasMail, fullDataBytes := "", "", []byte(nil)
	if deepMarshal, err := json.Marshal(member["deepData"]); err == nil {
		elems := gjson.ParseBytes(deepMarshal)
		raw := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(elems.Get("E-mail:").String(), "mailto:", "")))
		originalMail = raw
		aliasMail = importers.RetrieveAliasFromMail(raw)
		if aliasMail == "" {
			aliasMail = raw
		}
		// dm puo` essere un map[string]string ma anche un typed nil (quando
		// GetRegSocioDeepData fallisce dopo retry): in quel caso assegnare
		// una chiave panicherebbe. Skippiamo la riscrittura di full_data.
		if dm, ok := member["deepData"].(map[string]string); ok && dm != nil {
			dm["E-mail:"] = "mailto:" + aliasMail
			if b, err := json.Marshal(dm); err == nil {
				fullDataBytes = b
			}
		}
	}

	digest := memberDataDigest{
		Name:            name,
		City:            city,
		Birthdate:       birthdateStr,
		State:           state,
		Area:            area,
		OriginalMail:    originalMail,
		AliasMail:       aliasMail,
		FullData:        string(fullDataBytes),
		IsActive:        true,
		FullProfileLink: fullProfileLink,
	}
	newDataHash := computeDataHash(digest)

	img, _ := member["image"].(*filesystem.File)
	storedImageHash := newRecord.GetString("image_hash")
	newImageHash := storedImageHash
	if img != nil {
		if h := fileSHA256(img); h != "" {
			newImageHash = h
		}
	}

	// Forziamo l'update se la riga era stata disattivata altrove: il digest
	// porta sempre is_active=true (lo settiamo qui), quindi senza questo
	// override il riattivamento verrebbe ignorato.
	dataChanged := isNew || newDataHash == "" ||
		newDataHash != newRecord.GetString("data_hash") ||
		!newRecord.GetBool("is_active")
	imageChanged := img != nil && newImageHash != storedImageHash

	if !dataChanged && !imageChanged {
		return memberId
	}

	if dataChanged {
		newRecord.Set("name", name)
		newRecord.Set("city", city)
		newRecord.Set("birthdate", birthdate)
		newRecord.Set("state", state)
		newRecord.Set("area", area)
		newRecord.Set("original_mail", originalMail)
		newRecord.Set("alias_mail", aliasMail)
		if len(fullDataBytes) > 0 {
			newRecord.Set("full_data", fullDataBytes)
		}
		newRecord.Set("is_active", true)
		newRecord.Set("full_profile_link", fullProfileLink)
		if newDataHash != "" {
			newRecord.Set("data_hash", newDataHash)
		}
	}
	if imageChanged {
		newRecord.Set("image", img)
		newRecord.Set("image_hash", newImageHash)
	}

	if err := app.Save(newRecord); err != nil {
		app.Logger().Error("members sync: save failed", "id", newRecord.Id, "err", err)
	}

	return memberId
}

func GetMD5Hash(text string, salt string) string {
	normalized := NormalizeTextForHash(text)
	hash := md5.Sum([]byte(normalized + salt))
	return hex.EncodeToString(hash[:])
}

// NormalizeTextForHash applica una normalizzazione deterministica al testo prima dell'hash.
// Regole:
// - normalizza newline a \n
// - Unicode normal form: NFKC
// - trim spazi ai bordi
// - lowercase
// - collassa qualunque sequenza di whitespace Unicode in un singolo spazio
func NormalizeTextForHash(s string) string {
	if s == "" {
		return ""
	}

	// Normalizza newline: CRLF e CR -> LF
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Unicode normalization (compatibility decomposition + composition)
	s = norm.NFKC.String(s)

	// Trim e lowercase
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)

	// Collassa whitespace (incluse tab, newline, NBSP, ecc.)
	var b strings.Builder
	b.Grow(len(s))
	inSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inSpace {
				b.WriteByte(' ')
				inSpace = true
			}
			continue
		}
		inSpace = false
		b.WriteRune(r)
	}

	return strings.TrimSpace(b.String())
}

func NormalizeTextForHashAndRemoveSpecialCharsOrAccents(s string) string {
	// Variante di NormalizeTextForHash che, oltre alla normalizzazione deterministica,
	// rimuove anche:
	// - diacritici/accents (es. "è" -> "e", "Ł" -> "l" dove possibile)
	// - caratteri speciali/punteggiatura (mantiene solo lettere, numeri e spazi)
	//
	// È utile quando si vuole confrontare stringhe provenienti da fonti diverse
	// (OCR, input umano, sistemi esterni) minimizzando differenze di formattazione.
	if s == "" {
		return ""
	}

	// Prima applichiamo la normalizzazione base (newline, NFKC, trim/lowercase, whitespace).
	// Questo rende l'output deterministico e coerente con GetMD5Hash.
	s = NormalizeTextForHash(s)
	if s == "" {
		return ""
	}

	// Rimozione diacritici:
	// - passiamo a NFKD (decomposizione) così lettere + segni diacritici diventano rune separate.
	// - filtriamo i rune con categoria Mn (Mark, nonspacing) che rappresentano tipicamente i segni.
	// NB: non tutte le lettere "speciali" sono diacritici (es. ß, ø) e potrebbero non
	// convertire in ASCII; in quel caso restano come lettere e saranno gestite dal filtro successivo.
	d := norm.NFKD.String(s)

	var b strings.Builder
	b.Grow(len(d))
	inSpace := false

	for _, r := range d {
		// Filtra i combining marks (accenti/diacritici) dopo decomposizione.
		if unicode.Is(unicode.Mn, r) {
			continue
		}

		// Manteniamo solo lettere e numeri.
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			inSpace = false
			b.WriteRune(r)
			continue
		}

		// Trattiamo tutto il resto come separatore; collassiamo in un singolo spazio.
		if !inSpace {
			b.WriteByte(' ')
			inSpace = true
		}
	}

	// Trim finale per evitare spazi iniziali/finali e output vuoto " ".
	return strings.TrimSpace(b.String())
}

func SnapshotArea32Members(app core.App) {
	// Nome file deterministico e adatto come key S3
	now := time.Now()
	todayDateTime := now.Format("2006-01-02_15-04-05")

	allMembers, err := app.FindAllRecords("members_registry")
	if err != nil {
		return
	}

	activeMembers := 0
	for _, m := range allMembers {
		if m.GetBool("is_active") {
			activeMembers++
		}
	}
	inactiveMembers := len(allMembers) - activeMembers

	snapshotData := make([]map[string]any, 0, len(allMembers))
	for _, member := range allMembers {
		memberJson, err := member.MarshalJSON()
		if err != nil {
			continue
		}
		var memberMap map[string]any
		if err := json.Unmarshal(memberJson, &memberMap); err != nil {
			continue
		}
		snapshotData = append(snapshotData, memberMap)
	}

	marshaledSnapshot, err := json.Marshal(snapshotData)
	if err != nil {
		return
	}

	// Hash del contenuto (prima della compressione) utile per dedup/controlli di integrità.
	snapshotMD5 := md5.Sum(marshaledSnapshot)
	snapshotMD5Hex := hex.EncodeToString(snapshotMD5[:])

	compressed, err := cdnfiles.GzipCompressBytes(marshaledSnapshot, "members_registry.json")
	if err != nil {
		app.Logger().Error("gzip snapshot members_registry", "error", err)
		return
	}

	fileName := "snapshot_members/" + todayDateTime + ".json.gz"

	s3settings := app.Settings().S3
	if err := cdnfiles.UploadFileToS3(app, s3settings.Bucket, fileName, compressed, map[string]string{
		"content-type":            "application/gzip",
		"content-encoding":        "gzip",
		"original-filename":       "members_registry.json",
		"snapshot-created-at":     todayDateTime,
		"snapshot-created-at-iso": now.UTC().Format(time.RFC3339),
		"total-members":           strconv.Itoa(len(allMembers)),
		"active-members":          strconv.Itoa(activeMembers),
		"inactive-members":        strconv.Itoa(inactiveMembers),
		"snapshot-type":           "members_registry",
		"snapshot-format":         "json-array",
		"snapshot-md5":            snapshotMD5Hex,
		"created-by":              "mensadb-cron-snapshot",
	}); err != nil {
		app.Logger().Error("upload snapshot members_registry", "error", err)
		return
	}
}

// fileSHA256 ritorna lo SHA256 esadecimale dei bytes del file in memoria.
// Ritorna "" se il reader non e` apribile, cosi` il chiamante puo` decidere
// di non valorizzare image_hash e ricadere sul comportamento storico.
func fileSHA256(f *filesystem.File) string {
	if f == nil || f.Reader == nil {
		return ""
	}
	r, err := f.Reader.Open()
	if err != nil {
		return ""
	}
	defer r.Close()
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
