package hooks

import (
	"strings"
	"sync"
	"testing"
	"time"

	"mensadb/tools/dbtools"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestDeveloperMessagePreview(t *testing.T) {
	long := strings.Repeat("parola ", 40)

	cases := []struct {
		name, in, want string
	}{
		{"una riga corta resta com'e`", "Manutenzione stasera", "Manutenzione stasera"},
		{"due righe restano com'e`", "Riga uno\nRiga due", "Riga uno\nRiga due"},
		{"le righe vuote non contano", "Riga uno\n\n\nRiga due\n", "Riga uno\nRiga due"},
		{"oltre due righe si taglia", "Riga uno\nRiga due\nRiga tre", "Riga uno\nRiga due…"},
		{"il punto finale non si raddoppia", "Uno.\nDue.\nTre.", "Uno.\nDue…"},
	}
	for _, c := range cases {
		if got := developerMessagePreview(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}

	got := developerMessagePreview(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("testo lungo senza puntini: %q", got)
	}
	if n := len([]rune(got)); n > developerMessagePreviewRunes+1 {
		t.Errorf("testo lungo di %d caratteri", n)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "…"), "parol") {
		t.Errorf("parola tagliata a meta`: %q", got)
	}
}

func TestDeveloperMessageNotify(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	configs := core.NewBaseCollection("configs")
	configs.Fields.Add(&core.TextField{Name: "key"}, &core.TextField{Name: "value"})
	if err := app.Save(configs); err != nil {
		t.Fatal(err)
	}
	app.OnRecordAfterCreateSuccess("configs").BindFunc(DeveloperMessageCreateNotifyAsync)
	app.OnRecordAfterUpdateSuccess("configs").BindFunc(DeveloperMessageUpdateNotifyAsync)

	var mu sync.Mutex
	var sent []dbtools.PushNotification
	original := sendToAllUsers
	sendToAllUsers = func(_ core.App, n dbtools.PushNotification, _ ...bool) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, n)
	}
	defer func() { sendToAllUsers = original }()

	// takeSent aspetta le goroutine dell'hook e svuota quello che hanno mandato.
	takeSent := func() []dbtools.PushNotification {
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		out := sent
		sent = nil
		return out
	}

	save := func(r *core.Record, key, value string) *core.Record {
		t.Helper()
		if r == nil {
			r = core.NewRecord(configs)
		} else {
			// ricarica dal DB, come fa la dashboard prima di salvare
			fresh, err := app.FindRecordById(configs, r.Id)
			if err != nil {
				t.Fatal(err)
			}
			r = fresh
		}
		r.Set("key", key)
		r.Set("value", value)
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
		return r
	}

	other := save(nil, "notify_events_new", "true")
	if got := takeSent(); len(got) != 0 {
		t.Fatalf("un'altra chiave ha mandato %d notifiche", len(got))
	}

	msg := save(nil, developerMessageKey, "Riga uno\nRiga due\nRiga tre")
	got := takeSent()
	if len(got) != 1 {
		t.Fatalf("creazione: %d notifiche, attesa 1", len(got))
	}
	n := got[0]
	if n.TrTag != "" || n.Title != "Messaggio urgente" || n.Body != "Riga uno\nRiga due\nRiga tre" || n.PushBody != "Riga uno\nRiga due…" {
		t.Fatalf("notifica sbagliata: %+v", n)
	}

	save(msg, developerMessageKey, "Riga uno\nRiga due\nRiga tre")
	if got := takeSent(); len(got) != 0 {
		t.Fatalf("stesso valore: %d notifiche, attese 0", len(got))
	}

	save(msg, developerMessageKey, "Nuovo messaggio")
	if got := takeSent(); len(got) != 1 || got[0].Body != "Nuovo messaggio" {
		t.Fatalf("valore nuovo: %+v", got)
	}

	save(msg, developerMessageKey, "")
	if got := takeSent(); len(got) != 0 {
		t.Fatalf("valore vuoto: %d notifiche, attese 0", len(got))
	}

	save(other, developerMessageKey, "true")
	if got := takeSent(); len(got) != 1 {
		t.Fatalf("chiave diventata developer_message: %d notifiche, attesa 1", len(got))
	}
}
