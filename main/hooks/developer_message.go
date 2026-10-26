package hooks

import (
	"strings"
	"unicode/utf8"

	"mensadb/tools/dbtools"

	"github.com/pocketbase/pocketbase/core"
)

const (
	developerMessageKey   = "developer_message"
	developerMessageTitle = "Messaggio urgente"

	// Quanto del messaggio finisce nel testo della push: le prime righe, fino
	// a questo numero di righe e di caratteri. Il messaggio intero resta nella
	// notifica salvata, che si legge aprendola.
	developerMessagePreviewLines = 2
	developerMessagePreviewRunes = 140
)

// DeveloperMessageCreateNotifyAsync e DeveloperMessageUpdateNotifyAsync
// mandano a tutti gli utenti una notifica quando in `configs` la chiave
// developer_message riceve un valore nuovo: alla creazione del record, o
// quando cambia il value (o la key diventa developer_message). Un valore
// vuoto non manda niente, cosi` lo si puo` svuotare senza avvisare nessuno.
//
// Sono due funzioni perche` negli hook After*Success IsNew() e` gia` false
// anche dopo una creazione; Original() invece in un update ha ancora i
// valori di prima del salvataggio.
func DeveloperMessageCreateNotifyAsync(e *core.RecordEvent) error {
	notifyDeveloperMessage(e.App, e.Record, nil)
	return e.Next()
}

func DeveloperMessageUpdateNotifyAsync(e *core.RecordEvent) error {
	notifyDeveloperMessage(e.App, e.Record, e.Record.Original())
	return e.Next()
}

func notifyDeveloperMessage(app core.App, record, original *core.Record) {
	if record.GetString("key") != developerMessageKey {
		return
	}

	message := strings.TrimSpace(record.GetString("value"))
	if message == "" {
		return
	}

	if original != nil &&
		original.GetString("key") == developerMessageKey &&
		strings.TrimSpace(original.GetString("value")) == message {
		return
	}

	go func() {
		defer recoverAsyncHook(app, "DeveloperMessageNotify")
		sendToAllUsers(app, developerMessageNotification(message))
	}()
}

// sendToAllUsers e` una variabile per poterla sostituire nei test.
var sendToAllUsers = dbtools.SendPushNotificationToAllUsers

func developerMessageNotification(message string) dbtools.PushNotification {
	return dbtools.PushNotification{
		Title:    developerMessageTitle,
		Body:     message,
		PushBody: developerMessagePreview(message),
		Data: map[string]string{
			"type": "developer_message",
		},
	}
}

// developerMessagePreview tiene le prime righe non vuote del messaggio e
// aggiunge "…" se ha dovuto tagliare qualcosa.
func developerMessagePreview(message string) string {
	var lines []string
	for _, line := range strings.Split(message, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	truncated := len(lines) > developerMessagePreviewLines
	if truncated {
		lines = lines[:developerMessagePreviewLines]
	}
	preview := strings.Join(lines, "\n")

	if utf8.RuneCountInString(preview) > developerMessagePreviewRunes {
		preview = string([]rune(preview)[:developerMessagePreviewRunes])
		if i := strings.LastIndexAny(preview, " \n"); i > developerMessagePreviewRunes/2 {
			preview = preview[:i]
		}
		truncated = true
	}

	if truncated {
		return strings.TrimRight(preview, " \n.,;:") + "…"
	}
	return preview
}
