package hooks

import "github.com/pocketbase/pocketbase/core"

func ForceStampGen(e *core.RequestEvent) error {
	return e.Error(400, "This endpoint is not available", nil)
	eventId := e.Request.PathValue("id")

	if eventId == "" {
		return e.Error(400, "Event ID is required", nil)
	}

	record, err := e.App.FindRecordById("events", eventId)
	if err != nil {
		return err
	}

	bytesImage := createEventStamp(e.App, record)

	if bytesImage == nil {
		return e.Error(500, "Failed to create event stamp", nil)
	}

	return e.Blob(200, "image/png", bytesImage)
}
