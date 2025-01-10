package main

import (
	ics "github.com/arran4/golang-ical"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"strconv"
	"strings"
	"time"
)

type IcalEvents struct {
	Id             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Created        types.DateTime `json:"created"`
	Updated        types.DateTime `json:"updated"`
	WhenStart      types.DateTime `json:"when_start"`
	WhenEnd        types.DateTime `json:"when_end"`
	Location       string         `json:"location"`
	Lat            string         `json:"lat"`
	Lon            string         `json:"lon"`
	State          string         `json:"state"`
	Owner          string         `json:"owner"`
	OrganizerEmail string         `json:"organizer_email"`
	InfoLink       string         `json:"info_link"`
}

func (i *IcalEvents) GetSixDigitPrecisionLatLon() (string, string) {
	flat, _ := strconv.ParseFloat(i.Lat, 64)
	flon, _ := strconv.ParseFloat(i.Lon, 64)
	return strconv.FormatFloat(flat, 'f', 6, 64), strconv.FormatFloat(flon, 'f', 6, 64)
}

func RetrieveICAL(e *core.RequestEvent) error {
	hashCode := e.Request.PathValue("hash")

	resultCalendarStates, _ := app.FindAllRecords("calendar_link", dbx.NewExp("hash = {:user}", dbx.Params{
		"user": hashCode,
	}))
	if len(resultCalendarStates) == 0 {
		return e.String(404, "Calendar not found")
	}

	resultUsers, _ := app.FindAllRecords("users", dbx.NewExp("id = {:user}", dbx.Params{
		"user": resultCalendarStates[0].GetString("user"),
	}))
	if len(resultUsers) == 0 {
		return e.String(404, "User not found")
	}

	cal := ics.NewCalendar()
	cal.SetMethod(ics.MethodRequest)
	if !resultUsers[0].GetBool("is_membership_active") || resultUsers[0].GetDateTime("expire_membership").Time().Before(time.Now()) {
		event := cal.AddEvent("RENEW_MEMBERSHIP@svc.mensa.it")
		event.SetCreatedTime(time.Now())
		event.SetDtStampTime(time.Now())
		event.SetModifiedAt(time.Now())
		event.SetStartAt(time.Now().Add(time.Hour * 24))
		event.SetEndAt(time.Now().Add((time.Hour * 24) + (time.Hour * 1)))
		event.SetDescription("Rinnova la tua iscrizione a Mensa Italia")
		event.SetSummary("Rinnova la tua iscrizione a Mensa Italia!")
		event.SetURL("https://www.cloud32.it/Associazioni/utenti/rinnovo")
		event.SetOrganizer("tesoreria@mensa.it")
		e.Response.Header().Set("Content-Type", "text/calendar")
		return e.String(200, cal.Serialize())
	}

	var calendarStates = []interface{}{}

	for _, data := range resultCalendarStates[0].GetStringSlice("state") {
		calendarStates = append(calendarStates, data)
	}

	query := app.DB().Select(
		"events.id as id",
		"events.name as name",
		"events.description as description",
		"events.created as created",
		"events.updated as updated",
		"events.when_start as when_start",
		"events.when_end as when_end",
		"positions.name as location",
		"events.owner as owner",
		"events.info_link as info_link",
		"positions.name as location",
		"positions.state as state",
		"positions.lat as lat",
		"positions.lon as lon",
		"users.email as organizer_email",
	).From("events").InnerJoin("positions", dbx.NewExp("events.position = positions.id")).InnerJoin("users", dbx.NewExp("events.owner = users.id")).Where(
		dbx.Or(dbx.In("positions.state", calendarStates...), dbx.HashExp{"is_national": true}))

	var records []IcalEvents
	if err := query.All(&records); err != nil {
		return err
	}

	for _, record := range records {
		event := cal.AddEvent(record.Id + "@svc.mensa.it")
		event.SetCreatedTime(record.Created.Time())
		event.SetDtStampTime(record.Created.Time())
		event.SetModifiedAt(record.Updated.Time())
		event.SetStartAt(record.WhenStart.Time())
		event.SetEndAt(record.WhenEnd.Time())
		event.SetSummary(record.Name)
		event.SetDescription(strings.ReplaceAll(record.Description, "\r", " "))
		event.SetLocation(record.Location)
		lat, lon := record.GetSixDigitPrecisionLatLon()
		event.SetGeo(lat, lon)
		if record.InfoLink != "" {
			event.SetURL(record.InfoLink)
		}
		event.SetOrganizer(record.OrganizerEmail)
	}
	e.Response.Header().Set("Content-Type", "text/calendar")
	return e.String(200, cal.Serialize())

}
