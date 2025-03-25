package cs

import "github.com/pocketbase/pocketbase/core"

func example(e *core.RequestEvent) error {
	var bodyBytes []byte
	_, _ = e.Request.Body.Read(bodyBytes)

	e.JSON(200, map[string]interface{}{
		"Method": e.Request.Method,
		"URL":    e.Request.URL.String(),
		"Query":  e.Request.URL.Query(),
		"Body":   string(bodyBytes),
	})
}
