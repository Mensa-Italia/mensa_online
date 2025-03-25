package webhook

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/printful"
)

func PrintfulWebhookHandler(e *core.RequestEvent) error {
	var bodyBytes []byte
	_, _ = e.Request.Body.Read(bodyBytes)

	model := printful.ParseWebhookModel(bodyBytes)

	switch model.GetType() {
	case "product_synced":
		_, _ = printful.ParseProductSyncedWebhook(bodyBytes)
	case "product_updated":
		_, _ = printful.ParseProductUpdateWebhook(bodyBytes)
	case "product_deleted":
		_, _ = printful.ParseProductDeleteWebhook(bodyBytes)
		
	}

	return nil
}
