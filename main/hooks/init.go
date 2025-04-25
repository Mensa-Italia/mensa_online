package hooks

import "github.com/pocketbase/pocketbase/core"

func Load(app core.App) {

	app.OnRecordAfterUpdateSuccess("users").BindFunc(LogUserChart)
	app.OnRecordAfterCreateSuccess("addons").BindFunc(GeneratePublicPrivateKeys)
	app.OnRecordCreate("positions").BindFunc(PositionSetState)
	app.OnRecordCreate("ex_keys").BindFunc(OnKeyCreated)
	app.OnRecordAfterCreateSuccess("calendar_link").BindFunc(CalendarSetHash)
	app.OnRecordAfterCreateSuccess("events").BindFunc(EventsNotifyUsersAsync)
	app.OnRecordAfterCreateSuccess("deals").BindFunc(DealsNotifyUsersAsync)

}
