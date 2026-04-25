package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"mensadb/printful"
	"mensadb/tools/dbtools"
	"mensadb/tools/env"
)

func PrintfulWebhookHandler(e *core.RequestEvent) error {
	bodyBytes, err := io.ReadAll(e.Request.Body)
	if err != nil {
		e.App.Logger().Error("printful webhook: read body failed", "err", err)
		return e.String(http.StatusOK, "OK")
	}
	defer e.Request.Body.Close()

	if !verifyPrintfulSignature(e.Request.Header.Get("X-PF-Signature"), bodyBytes) {
		e.App.Logger().Warn("printful webhook: invalid signature, dropping",
			"ip", e.Request.RemoteAddr,
			"len", len(bodyBytes),
		)
		return e.String(http.StatusOK, "OK")
	}

	eventID := e.Request.Header.Get("X-PF-Webhook-Id")
	if eventID == "" {
		eventID = e.Request.Header.Get("X-PF-Webhook-ID")
	}
	if eventID == "" {
		sum := sha256.Sum256(bodyBytes)
		eventID = hex.EncodeToString(sum[:16])
	}

	if !dbtools.MarkWebhookEventProcessed(e.App, "printful", eventID) {
		e.App.Logger().Info("printful webhook: duplicate, skipping", "event_id", eventID)
		return e.String(http.StatusOK, "OK")
	}

	if err := printful.HandleWebhook(printful.PrintfulHandlers{
		ProductSyncedHandler:  handleBodyUpdate(e.App),
		ProductUpdatedHandler: handleBodyUpdate(e.App),
		ProductDeletedHandler: handleBodyDelete(e.App),
	}, bodyBytes); err != nil {
		e.App.Logger().Error("printful webhook: handler failed", "err", err)
	}

	return e.String(http.StatusOK, "OK")
}

func verifyPrintfulSignature(signature string, body []byte) bool {
	secret := env.GetPrintfulWebhookSecret()
	if secret == "" {
		// Se nessun secret è configurato, accettiamo (back-compat con setup attuali).
		// In produzione la env DEVE essere impostata.
		return true
	}
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

func handleBodyUpdate(app core.App) func(model printful.WebhookProductModel) error {
	return func(model printful.WebhookProductModel) error {
		boutiqueCollection, err := app.FindCollectionByNameOrId("boutique")
		if err != nil || boutiqueCollection == nil {
			app.Logger().Error("printful webhook: boutique collection not found", "err", err)
			return nil
		}
		record := core.NewRecord(boutiqueCollection)
		record.Set("name", model.Data.SyncProduct.Name)
		record.Set("description", model.Data.SyncProduct.Name)
		if err := app.Save(record); err != nil {
			app.Logger().Error("printful webhook: save record failed", "err", err)
		}
		return nil
	}
}

func handleBodyDelete(app core.App) func(model printful.WebhookProductModel) error {
	return func(model printful.WebhookProductModel) error {
		// TODO: implementare match per uid e cancellazione record. Per ora no-op silenzioso.
		_ = app
		_ = model
		return nil
	}
}
