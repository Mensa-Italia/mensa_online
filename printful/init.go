package printful

import (
	"time"

	"github.com/go-resty/resty/v2"
)

var restyClient = resty.New().SetTimeout(30 * time.Second)

func Setup(apiKey string) {
	restyClient = resty.New().SetTimeout(30 * time.Second).SetAuthToken(apiKey).SetBaseURL("https://api.printful.com")
}

func SetupWebhook(url string) {
	_, _ = restyClient.R().Delete("/webhooks")
	_, _ = restyClient.R().SetBody(
		map[string]interface{}{
			"url":   url,
			"types": webhookTypes,
		},
	).Post("/webhooks")
}
