package hooks

import "github.com/pocketbase/pocketbase/core"

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

}
