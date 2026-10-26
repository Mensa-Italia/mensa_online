package dbtools

import "testing"

func TestPushNotificationTexts(t *testing.T) {
	translate := func(key string) string { return "tr:" + key }

	tr := PushNotification{TrTag: "push_notification.new_deal", Title: "ignorato", Body: "ignorato"}
	if title, body := tr.texts(translate, true); title != "tr:push_notification.new_deal.title" || body != "tr:push_notification.new_deal.body" {
		t.Errorf("con TrTag: %q, %q", title, body)
	}

	free := PushNotification{Title: "Titolo", Body: "Testo intero", PushBody: "Testo…"}
	if title, body := free.texts(translate, false); title != "Titolo" || body != "Testo intero" {
		t.Errorf("salvata: %q, %q", title, body)
	}
	if title, body := free.texts(translate, true); title != "Titolo" || body != "Testo…" {
		t.Errorf("push: %q, %q", title, body)
	}

	noPushBody := PushNotification{Title: "Titolo", Body: "Testo"}
	if _, body := noPushBody.texts(translate, true); body != "Testo" {
		t.Errorf("push senza PushBody: %q", body)
	}
}
