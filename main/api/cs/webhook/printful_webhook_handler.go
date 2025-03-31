package webhook

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/printful"
)

func PrintfulWebhookHandler(e *core.RequestEvent) error {
	var bodyBytes []byte
	_, _ = e.Request.Body.Read(bodyBytes)

	printful.HandleWebhook(printful.PrintfulHandlers{
		ProductSyncedHandler:  handleBodyUpdate(e.App),
		ProductUpdatedHandler: handleBodyUpdate(e.App),
		ProductDeletedHandler: handleBodyDelete(e.App),
	}, bodyBytes)

	return nil
}

/*
	{
	  "collectionId": "pbc_3781977165",
	  "collectionName": "boutique",
	  "id": "test",
	  "uid": "test",
	  "name": "test",
	  "description": "test",
	  "image": [
	    "filename.jpg"
	  ],
	  "amount": 123,
	  "alternative_of": "RELATION_RECORD_ID",
	  "show": true,
	  "created": "2022-01-01 10:00:00.123Z",
	  "updated": "2022-01-01 10:00:00.123Z"
	}
*/
func handleBodyUpdate(app core.App) func(model printful.WebhookProductModel) error {
	return func(model printful.WebhookProductModel) error {
		boutiqueCollection, _ := app.FindCollectionByNameOrId("boutique")
		if boutiqueCollection == nil {
			record := core.NewRecord(boutiqueCollection)
			record.Set("name", model.Data.SyncProduct.Name)
			record.Set("description", model.Data.SyncProduct.Name)
		}
		return nil
	}

}

func handleBodyDelete(app core.App) func(model printful.WebhookProductModel) error {
	return func(model printful.WebhookProductModel) error {
		return nil
	}

}
