package exapp

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"mensadb/main/hooks"
	"mensadb/tools/dbtools"
	"strings"
	"time"
)

func StoreUserTickets(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "PUSH_PAYMENTS_DATA") {
		return e.String(401, "Unauthorized")
	}
	userId := e.Request.FormValue("user_id")
	var userRecord *core.Record
	var err error
	if userId != "" {
		userRecord, err = e.App.FindRecordById("users", userId)
	}
	if userId == "" || userRecord == nil {
		userEmail := e.Request.FormValue("user_email")
		userRecord, err = e.App.FindFirstRecordByFilter("users", "email={:user_email}", dbx.Params{"user_email": userEmail})
		if err != nil || userRecord == nil {
			userRecord, err = e.App.FindFirstRecordByFilter("members_registry", "full_data ~ {:user_email}", dbx.Params{"user_email": userEmail})
			if err != nil || userRecord == nil {
				fullname := e.Request.FormValue("user_fullname")
				possibleNames := getAllWordsCombinations(fullname)
				possibleIds := []string{}
				for _, nameVariant := range possibleNames {
					userRecord, err = e.App.FindFirstRecordByFilter("members_registry", "name:lower ~ {:name_variant}", dbx.Params{"name_variant": strings.ToLower(nameVariant)})
					if err == nil && userRecord != nil {
						possibleIds = append(possibleIds, userRecord.Id)
					}
				}
				if len(possibleIds) == 1 {
					userRecord, err = e.App.FindRecordById("users", possibleIds[0])
					if err != nil || userRecord == nil {
						return e.InternalServerError("User not found", nil)
					}
				} else {
					return e.InternalServerError("User not found", nil)
				}
			}
		}
	}
	collection, _ := e.App.FindCollectionByNameOrId("tickets")
	var purchaseRecord *core.Record
	purchaseRecord, err = e.App.FindRecordById(collection, e.Request.FormValue("unique_id"))
	if purchaseRecord == nil || err != nil {
		purchaseRecord = core.NewRecord(collection)
	}
	purchaseRecord.Set("id", e.Request.FormValue("unique_id"))
	purchaseRecord.Set("name", e.Request.FormValue("name"))
	purchaseRecord.Set("user_id", userRecord.Id)
	purchaseRecord.Set("link", e.Request.FormValue("link"))
	purchaseRecord.Set("qr", e.Request.FormValue("qr"))
	purchaseRecord.Set("description", e.Request.FormValue("description"))
	purchaseRecord.Set("start_date", e.Request.FormValue("start_date"))
	purchaseRecord.Set("customer_data", e.Request.FormValue("customer_data"))

	if e.Request.FormValue("end_date") == "" {
		deadlineString := e.Request.FormValue("start_date")
		deadlineTime, err := time.Parse(time.RFC3339, deadlineString)
		if err == nil {
			deadlineTime = deadlineTime.Add(24 * time.Hour)
			deadlineString = deadlineTime.Format(time.RFC3339)
			purchaseRecord.Set("deadline", deadlineString)
		} else {
			purchaseRecord.Set("deadline", e.Request.FormValue("start_date"))
		}
	} else {
		purchaseRecord.Set("end_date", e.Request.FormValue("end_date"))
		purchaseRecord.Set("deadline", e.Request.FormValue("end_date"))

	}

	if e.Request.FormValue("event_id") != "" {
		purchaseRecord.Set("internal_ref_id", "event:"+e.Request.FormValue("event_id"))
	}

	err = e.App.Save(purchaseRecord)

	go func() {
		dbtools.SendPushNotificationToUser(e.App, dbtools.PushNotification{
			UserId: userRecord.Id,
			TrTag:  "push_notification.ticket_purchase_recorded",
			TrNamedParams: map[string]string{
				"name": e.Request.FormValue("name"),
			},
			Data: map[string]string{
				"type": "ticket_purchase",
			},
		})
	}()

	if err != nil {
		return e.InternalServerError("Failed to save purchase record", err)
	}

	return e.String(200, "OK")

}

func getAllWordsCombinations(words string) []string {
	// Divide la stringa in parole usando lo spazio come separatore
	parts := strings.Fields(words) // gestisce anche spazi multipli

	var result []string
	n := len(parts)

	// backtracking per generare tutte le permutazioni
	var backtrack func(path []string, used []bool)
	backtrack = func(path []string, used []bool) {
		if len(path) == n {
			// unisci le parole con "-" come da esempio
			result = append(result, strings.Join(path, " "))
			return
		}
		for i := 0; i < n; i++ {
			if used[i] {
				continue
			}
			used[i] = true
			path = append(path, parts[i])

			backtrack(path, used)

			// backtrack
			path = path[:len(path)-1]
			used[i] = false
		}
	}

	if n == 0 {
		return result
	}

	used := make([]bool, n)
	backtrack([]string{}, used)

	return result
}
