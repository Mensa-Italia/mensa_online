package exapp

import (
	"mensadb/main/hooks"
	"mensadb/tools/dbtools"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func StoreUserTickets(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "PUSH_PAYMENTS_DATA") {
		return e.String(401, "Unauthorized")
	}
	userId := e.Request.FormValue("attendee_user_id")
	userEmail := e.Request.FormValue("attendee_user_email")
	fullname := e.Request.FormValue("attendee_user_fullname")
	resolvedUserId, err := userReconciliationFunction(e.App, userId, fullname, userEmail)
	if err != nil || resolvedUserId == "" {
		userId = e.Request.FormValue("user_id")
		userEmail = e.Request.FormValue("user_email")
		fullname = e.Request.FormValue("user_fullname")
		resolvedUserId, err = userReconciliationFunction(e.App, userId, fullname, userEmail)
		if err != nil {
			return e.InternalServerError("User reconciliation failed", err)
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
	purchaseRecord.Set("user_id", resolvedUserId)
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
			UserId: resolvedUserId,
			TrTag:  "push_notification.ticket_purchase_recorded",
			TrNamedParams: map[string]string{
				"name": e.Request.FormValue("name") + " - " + e.Request.FormValue("description"),
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

func RemoveUserTickets(e *core.RequestEvent) error {
	authKey := e.Request.Header.Get("Authorization")
	if !hooks.CheckKey(e.App, authKey, "PUSH_PAYMENTS_DATA") {
		return e.String(401, "Unauthorized")
	}
	collection, _ := e.App.FindCollectionByNameOrId("tickets")
	purchaseRecord, err := e.App.FindRecordById(collection, e.Request.FormValue("unique_id"))
	if err != nil || purchaseRecord == nil {
		return e.String(404, "Purchase record not found")
	}
	err = e.App.Delete(purchaseRecord)
	if err != nil {
		return e.InternalServerError("Failed to delete purchase record", err)
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

func userReconciliationFunction(app core.App, userId, fullName, email string) (string, error) {
	var userRecord *core.Record
	var err error
	if userId != "" {
		userRecord, err = app.FindRecordById("members_registry", userId)
	}
	if userId == "" || userRecord == nil {
		userEmail := email
		userRecord, err = app.FindFirstRecordByFilter("users", "email={:user_email}", dbx.Params{"user_email": userEmail})
		if err != nil || userRecord == nil {
			userRecord, err = app.FindFirstRecordByFilter("members_registry", "full_data ~ {:user_email}", dbx.Params{"user_email": userEmail})
			if err != nil || userRecord == nil {
				possibleNames := getAllWordsCombinations(fullName)
				possibleIds := []string{}
				for _, nameVariant := range possibleNames {
					userRecord, err = app.FindFirstRecordByFilter("members_registry", "name:lower ~ {:name_variant}", dbx.Params{"name_variant": strings.ToLower(nameVariant)})
					if err == nil && userRecord != nil {
						possibleIds = append(possibleIds, userRecord.Id)
					}
				}
				if len(possibleIds) == 1 {
					userRecord, err = app.FindRecordById("users", possibleIds[0])
					if err != nil || userRecord == nil {
						return "", err
					}
				} else {
					return "", nil
				}
			}
		}
	}

	return userRecord.Id, nil
}
