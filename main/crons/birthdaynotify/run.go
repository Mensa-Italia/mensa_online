package birthdaynotify

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"

	"mensadb/tools/dbtools"
)

const (
	// enabledKey accende le push: finche` non vale "true" il cron non manda
	// niente, cosi` si accende solo dopo aver caricato le chiavi su Tolgee.
	enabledKey = "notify_birthdays"
	// lastDayKey e` l'ultimo giorno (di Roma) gia` festeggiato: un riavvio
	// o un secondo giro nello stesso giorno non rimanda gli auguri.
	lastDayKey = "birthday_notify_last_day"
	trTag      = "push_notification.birthday"
)

var rome = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Rome"); err == nil {
		return loc
	}
	return time.UTC
}()

// Run manda gli auguri ai soci che compiono gli anni oggi. Nell'app la push
// (`type: birthday`) fa salire la scheda degli auguri.
//
// Il registro salva la data di nascita come mezzanotte italiana convertita
// in UTC (chi e` nato il 26 ottobre ha `…-10-25 22:00:00Z`), quindi il giorno
// va letto nel fuso di Roma, come fa `Birthdays` nell'app. Tocca solo a chi
// ha l'app con un dispositivo registrato e non ha spento `notify_general`.
func Run(app core.App, now time.Time) (int, error) {
	if dbtools.GetInternalConfig(app, enabledKey) != "true" {
		return 0, nil
	}
	today := now.In(rome).Format(time.DateOnly)
	if dbtools.GetInternalConfig(app, lastDayKey) == today {
		return 0, nil
	}

	var rows []struct {
		ID        string `db:"id"`
		Name      string `db:"name"`
		Birthdate string `db:"birthdate"`
	}
	err := app.DB().NewQuery(`
		SELECT m.id, m.name, m.birthdate FROM members_registry m
		WHERE m.is_active = TRUE AND m.birthdate != ''
		  AND EXISTS (SELECT 1 FROM users_devices d WHERE d.user = m.id AND d.firebase_id != '')
		  AND NOT EXISTS (SELECT 1 FROM users_metadata um
		                  WHERE um.user = m.id AND um.key = 'notify_general' AND um.value = 'false')`).All(&rows)
	if err != nil {
		return 0, fmt.Errorf("members_registry: %w", err)
	}

	sent := 0
	for _, r := range rows {
		born, err := types.ParseDateTime(r.Birthdate)
		if err != nil || born.IsZero() {
			continue
		}
		age, ok := birthday(born.Time(), now)
		if !ok {
			continue
		}
		dbtools.SendPushNotificationToUser(app, message(r.ID, r.Name, age))
		sent++
	}

	if err := dbtools.SetInternalConfig(app, lastDayKey, today); err != nil {
		return sent, fmt.Errorf("salvataggio %s: %w", lastDayKey, err)
	}
	app.Logger().Info("[CRON] birthdaynotify: auguri mandati", "day", today, "sent", sent)
	return sent, nil
}

// birthday dice se oggi (a Roma) e` il compleanno di chi e` nato in born, e
// quanti anni compie: 0 se la data non e` credibile (anno futuro, 1900).
// Chi e` nato il 29 febbraio festeggia il 28 negli anni non bisestili.
func birthday(born, now time.Time) (int, bool) {
	b, t := born.In(rome), now.In(rome)
	same := b.Month() == t.Month() && b.Day() == t.Day()
	leap := b.Month() == time.February && b.Day() == 29 &&
		t.Month() == time.February && t.Day() == 28 && !isLeap(t.Year())
	if !same && !leap {
		return 0, false
	}
	age := t.Year() - b.Year()
	if age < 1 || age > 120 {
		age = 0
	}
	return age, true
}

func message(userID, fullName string, age int) dbtools.PushNotification {
	name := firstName(fullName)
	params := map[string]string{"name": name}
	body := "Tutta Mensa Italia ti augura una splendida giornata."
	if age > 0 {
		params["age"] = strconv.Itoa(age)
		body = "Oggi compi " + params["age"] + " anni: tutta Mensa Italia ti augura una splendida giornata."
	}
	return dbtools.PushNotification{
		UserId:        userID,
		TrTag:         trTag,
		TrNamedParams: params,
		Data:          map[string]string{"type": "birthday"},
		// Testo di riserva finche` le chiavi non sono su Tolgee.
		Title: "Buon compleanno, " + name + "! 🎂",
		Body:  body,
	}
}

// firstName: il registro scrive "Nome Cognome", spesso tutto maiuscolo.
func firstName(full string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(full), " ")
	if first == "" {
		return ""
	}
	r := []rune(strings.ToLower(first))
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func isLeap(y int) bool { return (y%4 == 0 && y%100 != 0) || y%400 == 0 }
