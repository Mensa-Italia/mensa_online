package dbtools

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"log"
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

	// Recupera i nuovi membri da Area32 che non sono già nel database
	newMembers, _ := scraperApi.GetAllRegSoci()
	// Aggiorna i membri in modo concorrente
	allMembersIDs := []string{}
	for _, member := range newMembers {
		allMembersIDs = append(allMembersIDs, UpdateMembers(app, member))
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

	// Costruisce un elenco degli ID dei membri esistenti
	membersUids := []string{}
	for _, member := range membersInside {
		membersUids = append(membersUids, member.Id)
	}

	// per i memberi in memberUids che non sono in allMembersIDs imposto is_active a false
	for _, member := range membersUids {
		found := false
		for _, memberId := range allMembersIDs {
			if member == memberId {
				found = true
				break
			}
		}
		if !found {
			memberInside, err := app.FindRecordById(membersRegistryCollection, member)
			if err == nil {
				memberInside.Set("is_active", false)
				err = app.Save(memberInside)
				if err != nil {
					log.Println("Error saving member: ", err.Error())
				}
			}
			userInside, err := app.FindRecordById("users", member)
			if err == nil {
				userInside.Set("is_membership_active", false)
				_ = app.Save(userInside)
			}
		} else {
			userInside, err := app.FindRecordById("users", member)
			if err == nil && userInside.GetBool("is_membership_active") == false {
				userInside.Set("is_membership_active", true)
				_ = app.Save(userInside)
			}
		}
	}

}

// Funzione per aggiornare i membri nel database
func UpdateMembers(app core.App, member map[string]any) string {
	// Recupera la collezione "members" dal database
	id, err := app.FindCollectionByNameOrId("members_registry")
	if err != nil {
		return ""
	}
	memberId := member["uid"].(string)
	// Controlla se il membro esiste già nel database
	newRecord, err := app.FindRecordById(id, memberId)
	if err != nil {
		newRecord = core.NewRecord(id)
		newRecord.Id = member["uid"].(string)
	}
	newRecord.Set("name", member["name"].(string))
	newRecord.Set("city", member["city"].(string))
	newRecord.Set("birthdate", member["birthDate"])
	newRecord.Set("state", member["state"].(string))
	newRecord.Set("area", member["area"].(string))
	marshal, err := json.Marshal(member["deepData"])
	if err == nil {
		elems := gjson.ParseBytes(marshal)
		newRecord.Set("original_mail", strings.ToLower(strings.TrimSpace(strings.ReplaceAll(elems.Get("E-mail:").String(), "mailto:", ""))))
		alias := importers.RetrieveAliasFromMail(strings.ToLower(strings.TrimSpace(strings.ReplaceAll(elems.Get("E-mail:").String(), "mailto:", ""))))
		newRecord.Set("alias_mail", alias)

		member["deepData"].(map[string]string)["E-mail:"] = "mailto:" + alias
		marshal, err := json.Marshal(member["deepData"])
		if err == nil {
			newRecord.Set("full_data", marshal)
		}
	}
	if member["image"].(*filesystem.File) != nil {
		newRecord.Set("image", member["image"].(*filesystem.File))
	}
	newRecord.Set("is_active", true)
	newRecord.Set("full_profile_link", member["full_profile_link"])
	// Salva il record nel database
	err = app.Save(newRecord)
	if err != nil {
		log.Println("Error saving member: ", err.Error())
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
	allMembers, err := app.FindAllRecords("members_registry")
	if err != nil {
		return
	}

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

	compressed, err := cdnfiles.GzipCompressBytes(marshaledSnapshot, "members_registry.json")
	if err != nil {
		app.Logger().Error("gzip snapshot members_registry", err)
		return
	}

	// Nome file deterministico e adatto come key S3
	todayDateTime := time.Now().Format("2006-01-02_15-04-05")
	fileName := "snapshot_members/" + todayDateTime + ".json.gz"

	s3settings := app.Settings().S3
	if err := cdnfiles.UploadFileToS3(app, s3settings.Bucket, fileName, compressed, map[string]string{
		"x-amz-meta-content-type":        "application/gzip",
		"x-amz-meta-content-encoding":    "gzip",
		"x-amz-meta-original-filename":   "members_registry.json",
		"x-amz-meta-snapshot-created-at": todayDateTime,
		"x-amz-meta-total-members":       strconv.Itoa(len(allMembers)),
		"x-amz-meta-snapshot-type":       "members_registry",
		"x-amz-meta-created-by":          "mensadb-cron-snapshot",
	}); err != nil {
		app.Logger().Error("upload snapshot members_registry", err)
		return
	}
}
