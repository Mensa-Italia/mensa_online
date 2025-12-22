package dbtools

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"log"
	"mensadb/area32"
	"mensadb/importers"
	"mensadb/tools/env"
	"strings"
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

func GetMD5Hash(text string) string {
	normalized := NormalizeTextForHash(text)
	hash := md5.Sum([]byte(normalized))
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
