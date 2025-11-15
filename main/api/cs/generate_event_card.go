package cs

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/aipower"
)

func GenerateEventCardHandler(e *core.RequestEvent) error {

	title := e.Request.URL.Query().Get("title")
	line0 := e.Request.URL.Query().Get("line0")
	line1 := e.Request.URL.Query().Get("line1")
	line2 := e.Request.URL.Query().Get("line2")
	line3 := e.Request.URL.Query().Get("line3")
	line4 := e.Request.URL.Query().Get("line4")

	card, err := aipower.GenerateEventCard(title, [5]string{
		line0,
		line1,
		line2,
		line3,
		line4,
	})
	if err != nil {
		return err
	}

	return e.Blob(200, "image/jpeg", card)
}
