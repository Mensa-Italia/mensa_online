package printful

import "encoding/json"

type WebhookModel struct {
	Type    string `json:"type"`
	Created int    `json:"created"`
	Retries int    `json:"retries"`
	Store   int    `json:"store"`
}

func (w *WebhookModel) GetStore() int {
	return w.Store
}

func (w *WebhookModel) GetType() string {
	return w.Type
}

func (w *WebhookModel) GetCreated() int {
	return w.Created
}

func (w *WebhookModel) GetRetries() int {
	return w.Retries
}

func ParseWebhookModel(bodyBytes []byte) *WebhookModel {
	var webhookModel WebhookModel
	_ = json.Unmarshal(bodyBytes, &webhookModel)
	return &webhookModel
}
