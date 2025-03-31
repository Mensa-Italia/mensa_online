package printful

type PrintfulHandlers struct {
	ProductSyncedHandler  func(model WebhookProductModel) error
	ProductUpdatedHandler func(model WebhookProductModel) error
	ProductDeletedHandler func(model WebhookProductModel) error
}

func HandleWebhook(handlers PrintfulHandlers, bodyBytes []byte) error {
	model := ParseWebhookModel(bodyBytes)

	switch model.GetType() {
	case "product_synced":
		webhookModel, err := ParseProductSyncedWebhook(bodyBytes)
		if err != nil {
			return err
		}
		return handlers.ProductSyncedHandler(*webhookModel)
	case "product_updated":
		webhookModel, err := ParseProductUpdateWebhook(bodyBytes)
		if err != nil {
			return err
		}
		return handlers.ProductUpdatedHandler(*webhookModel)
	case "product_deleted":
		webhookModel, err := ParseProductDeleteWebhook(bodyBytes)
		if err != nil {
			return err
		}
		return handlers.ProductDeletedHandler(*webhookModel)
	}

	return nil
}
