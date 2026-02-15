package dbtools

import (
	"fmt"
	"log/slog"
	"mensadb/tools/zauth"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

var fullDataKeyMap = map[string]string{
	"Cap:":           "cap",
	"Cellulare:":     "cellulare",
	"Città:":         "citta",
	"E-mail:":        "email",
	"FAX:":           "fax",
	"Facebook ID:":   "facebook_id",
	"Indirizzo:":     "indirizzo",
	"PEC:":           "pec",
	"Professione:":   "professione",
	"Provincia:":     "provincia",
	"Sito:":          "sito",
	"Telefono:":      "telefono",
	"Titolo Studio:": "titolo_studio",
}

func mapFullDataKey(k string) (string, bool) {
	// trim per sicurezza (spazi accidentali)
	k = strings.TrimSpace(k)
	out, ok := fullDataKeyMap[k]
	return out, ok
}

func UpdateZitadel(app core.App) {
	records, err := app.FindAllRecords("zitadel_extractor")
	if err != nil {
		return
	}

	for _, record := range records {
		full := map[string]any{}
		if err := record.UnmarshalJSONField("full_data", &full); err != nil {
			slog.Warn("failed to unmarshal full_data, using empty", "record_id", record.Id, "error", err)
			full = map[string]any{}
		}

		var Metadata map[string]string
		metadata := make(map[string]string, len(full)+8)

		for k, v := range full {
			mk, ok := mapFullDataKey(k)
			if !ok {
				slog.Warn("unexpected full_data key", "record_id", record.Id, "key", k)
				continue // oppure: metadata["full_unknown_"+sanitizeKey(k)] = fmt.Sprint(v)
			}
			metadata[mk] = fmt.Sprint(v)
		}

		// campi strutturati
		metadata["city"] = record.GetString("city")
		metadata["state"] = record.GetString("state")
		metadata["area"] = record.GetString("area")
		metadata["expire_membership"] = record.GetString("expire_membership")
		metadata["is_active"] = fmt.Sprint(record.GetBool("is_active"))
		metadata["is_membership_active"] = fmt.Sprint(record.GetBool("is_membership_active"))
		metadata["birthdate"] = record.GetString("birthdate")
		metadata["avatar"] = record.GetString("avatar")

		zauth.CreateUser(
			record.GetString("name"),
			record.GetString("alias_mail"),
			record.GetString("original_mail"),
			Metadata,
		)

	}
}
