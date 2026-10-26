package printful

import "encoding/json"

type WebhookProductModel struct {
	WebhookModel
	Data struct {
		SyncProduct struct {
			Id           int    `json:"id"`
			ExternalId   string `json:"external_id"`
			Name         string `json:"name"`
			Variants     int    `json:"variants"`
			Synced       int    `json:"synced"`
			ThumbnailUrl string `json:"thumbnail_url"`
			IsIgnored    bool   `json:"is_ignored"`
		} `json:"sync_product"`
	} `json:"data"`
}

func ParseProductSyncedWebhook(body []byte) (*WebhookProductModel, error) {
	var webhook WebhookProductModel
	err := json.Unmarshal(body, &webhook)
	if err != nil {
		return nil, err
	}
	return &webhook, nil
}

func ParseProductUpdateWebhook(body []byte) (*WebhookProductModel, error) {
	var webhook WebhookProductModel
	err := json.Unmarshal(body, &webhook)
	if err != nil {
		return nil, err
	}
	return &webhook, nil
}

func ParseProductDeleteWebhook(body []byte) (*WebhookProductModel, error) {
	var webhook WebhookProductModel
	err := json.Unmarshal(body, &webhook)
	if err != nil {
		return nil, err
	}
	return &webhook, nil
}
