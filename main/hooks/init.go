package hooks

import (
	"log"
	"mensadb/tools/aipower"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

func Load(app core.App) {

	app.OnRecordAfterUpdateSuccess("users").BindFunc(LogUserChart)
	app.OnRecordAfterCreateSuccess("addons").BindFunc(GeneratePublicPrivateKeys)
	app.OnRecordCreate("positions").BindFunc(PositionSetState)
	app.OnRecordCreate("ex_keys").BindFunc(OnKeyCreated)
	app.OnRecordAfterCreateSuccess("calendar_link").BindFunc(CalendarSetHash)

	// Notify users when an event is created
	app.OnRecordAfterCreateSuccess("events").BindFunc(EventsNotifyUsersAsync)
	app.OnRecordAfterUpdateSuccess("events").BindFunc(EventsUpdateNotifyUsersAsync)

	// Notify users when a deal is created or updated
	app.OnRecordAfterCreateSuccess("deals").BindFunc(DealsNotifyUsersAsync)
	app.OnRecordAfterUpdateSuccess("deals").BindFunc(DealsUpdateNotifyUsersAsync)

	app.OnRecordAfterUpdateSuccess("stamp").BindFunc(StampUpdateImageAsync)

}

func StampUpdateImageAsync(e *core.RecordEvent) error {
	record := e.Record

	if strings.Contains(record.GetString("description"), "[UPDATE]") {

		// Generazione dell'immagine del timbro
		geminiImage, err := aipower.GenerateStamp(record.GetString("description"), false)
		if err != nil {
			// Log dell'errore nella generazione dello stamp
			log.Printf("Errore nella generazione dello stamp: %v", err)
			return e.Next()
		}
		fileImage, err := filesystem.NewFileFromBytes(geminiImage, "stamp.png")
		if err != nil {
			log.Printf("Errore nella creazione del file immagine: %v", err)
			return e.Next()
		}
		record.Set("image", fileImage)

		record.Set("description", strings.TrimSpace(strings.ReplaceAll(record.GetString("description"), "[UPDATE]", "")))

		_ = e.App.Save(record)

	}

	return e.Next()
}
