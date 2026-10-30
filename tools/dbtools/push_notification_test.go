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

// Chiave senza traduzione: il testo di riserva, se c'e`, vince sulla chiave
// nuda; senza riserva resta la chiave, come prima.
func TestPushNotificationTextsFallback(t *testing.T) {
	missing := func(key string) string { return key }

	withFallback := PushNotification{TrTag: "leaderboard.notify.up.1", Title: "Titolo", Body: "Testo", PushBody: "Breve"}
	if title, body := withFallback.texts(missing, false); title != "Titolo" || body != "Testo" {
		t.Errorf("salvata: %q, %q", title, body)
	}
	if _, body := withFallback.texts(missing, true); body != "Breve" {
		t.Errorf("push: %q", body)
	}

	bare := PushNotification{TrTag: "leaderboard.notify.up.1"}
	if title, body := bare.texts(missing, true); title != "leaderboard.notify.up.1.title" || body != "leaderboard.notify.up.1.body" {
		t.Errorf("senza riserva: %q, %q", title, body)
	}
}
