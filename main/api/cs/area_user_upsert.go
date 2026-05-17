package cs

import (
	"context"
	"errors"
	"log"
	"mensadb/area32"
	"mensadb/importers"
	"mensadb/tools/generic"
	"mensadb/tools/payment"
	"mensadb/tools/zauth"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// upsertUserFromAreaUser crea o aggiorna il record PB users a partire dai
// dati Area32. Riusato da AuthWithAreaHandler e AuthWithZitadelHandler.
// Avvia anche i side-effect asincroni (Stripe customer + Zitadel password).
func upsertUserFromAreaUser(app core.App, areaUser *area32.Area32User, email, password string) (*core.Record, error) {
	byUser, err := app.FindRecordById("users", areaUser.Id)

	if byUser == nil || err != nil {
		collection, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return nil, err
		}
		newUser := core.NewRecord(collection)
		newUser.Set("id", areaUser.Id)
		newUser.SetEmail(email)
		newUser.Set("username", suggestUniqueAuthRecordUsername(app, "users", strings.Split(email, "@")[0]))
		newUser.SetPassword(password)
		newUser.SetVerified(true)
		newUser.Set("name", areaUser.Fullname)
		newUser.Set("expire_membership", areaUser.ExpireDate)
		newUser.Set("is_membership_active", areaUser.IsMembershipActive)

		powerList := []string{}
		if areaUser.IsATestMaker {
			powerList = append(powerList, "testmakers")
		}
		segretari := importers.RetrieveForwardedMail("segretari")
		if slices.Contains(segretari, email) {
			powerList = append(powerList, "events")
		}
		if len(powerList) > 0 {
			newUser.Set("powers", powerList)
		}

		log.Println(areaUser.ImageUrl)
		fileImage, err := filesystem.NewFileFromURL(context.Background(), areaUser.ImageUrl)
		if err == nil {
			newUser.Set("avatar", fileImage)
		}

		if err := app.Save(newUser); err != nil {
			log.Println("Invalid credentials on new save", err)
			return nil, err
		}

		calendarLinkCollection, _ := app.FindCollectionByNameOrId("calendar_link")
		newCalendar := core.NewRecord(calendarLinkCollection)
		newCalendar.Set("user", areaUser.Id)
		newCalendar.Set("hash", generic.RandomHash())
		_ = app.Save(newCalendar)

		go func() {
			_, _ = payment.GetCustomerId(app, areaUser.Id)
		}()
		go func() {
			zauth.SetUserPassword(areaUser.Id, password)
		}()
		return newUser, nil
	}

	byUser.SetEmail(email)
	byUser.SetVerified(true)
	byUser.Set("name", areaUser.Fullname)
	byUser.Set("expire_membership", areaUser.ExpireDate)
	byUser.SetPassword(password)
	byUser.Set("is_membership_active", areaUser.IsMembershipActive)

	powers := byUser.GetStringSlice("powers")
	if areaUser.IsATestMaker && !slices.Contains(powers, "testmakers") {
		powers = append(powers, "testmakers")
		byUser.Set("powers", powers)
	} else if !areaUser.IsATestMaker && slices.Contains(powers, "testmakers") {
		powers = removeFromSlice(powers, "testmakers")
		byUser.Set("powers", powers)
	}

	if err := app.Save(byUser); err != nil {
		log.Println("Invalid credentials on update", err)
		return nil, err
	}

	byUser, err = app.FindRecordById("users", areaUser.Id)
	if err != nil || byUser == nil || !byUser.ValidatePassword(password) {
		if byUser != nil {
			data, _ := byUser.MarshalJSON()
			log.Println("Invalid credentials on reload", string(data))
		}
		if err == nil {
			err = errors.New("password validation failed after upsert")
		}
		return nil, err
	}

	go func() {
		_, _ = payment.GetCustomerId(app, areaUser.Id)
	}()
	go func() {
		zauth.SetUserPassword(areaUser.Id, password)
	}()
	return byUser, nil
}
