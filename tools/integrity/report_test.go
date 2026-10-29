package integrity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// Il corpo di esempio del contratto: i nomi dei campi sono quelli che manda il
// client, e un report ben formato passa da Clean senza cambiare.
const contractReport = `{
  "platform": "ios",
  "stage": "attest_key",
  "domain": "com.apple.devicecheck.error",
  "code": 2,
  "detail": "The operation couldn't be completed.",
  "underlying": "NSOSStatusErrorDomain/-1 Invalid appUUID",
  "disposition": "discard_key",
  "outcome": "failed",
  "attempt": 1,
  "key_phase": "pending",
  "key_age_s": 0,
  "elapsed_ms": 150,
  "app": "31000984",
  "os": "iOS 27.0.1 (24A446)",
  "ios_app_on_mac": false
}`

func TestDeviceReportContractRoundTrip(t *testing.T) {
	var r DeviceReport
	if err := json.Unmarshal([]byte(contractReport), &r); err != nil {
		t.Fatal(err)
	}
	want := DeviceReport{
		Platform: "ios", Stage: "attest_key", Domain: "com.apple.devicecheck.error", Code: 2,
		Detail: "The operation couldn't be completed.", Underlying: "NSOSStatusErrorDomain/-1 Invalid appUUID",
		Disposition: "discard_key", Outcome: "failed", Attempt: 1, KeyPhase: "pending",
		KeyAgeS: 0, ElapsedMs: 150, App: "31000984", OS: "iOS 27.0.1 (24A446)",
	}
	if r != want {
		t.Fatalf("decodifica: %+v", r)
	}
	if got := r.Clean(); got != want {
		t.Fatalf("un report valido non deve cambiare: %+v", got)
	}
	if len(contractReport) > MaxReportBytes {
		t.Fatalf("l'esempio del contratto (%d byte) deve stare nel tetto di %d", len(contractReport), MaxReportBytes)
	}
}

func TestCleanClosedLists(t *testing.T) {
	cases := []struct {
		name string
		in   DeviceReport
		want DeviceReport // solo i campi a elenco chiuso
	}{
		{"valori noti", DeviceReport{Platform: "android", Stage: "generate_key", Disposition: "unsupported", Outcome: "rotated", KeyPhase: "ready"},
			DeviceReport{Platform: "android", Stage: "generate_key", Disposition: "unsupported", Outcome: "rotated", KeyPhase: "ready"}},
		{"maiuscole e spazi", DeviceReport{Platform: " IOS ", Stage: "Assertion", Disposition: " Transient", Outcome: "FAILED", KeyPhase: "None "},
			DeviceReport{Platform: "ios", Stage: "assertion", Disposition: "transient", Outcome: "failed", KeyPhase: "none"}},
		{"fuori elenco", DeviceReport{Platform: "symbian", Stage: "boh", Disposition: "x", Outcome: "y", KeyPhase: "z"},
			DeviceReport{Platform: "?", Stage: "other", Disposition: "other", Outcome: "other", KeyPhase: "other"}},
		{"assenti: i facoltativi restano vuoti", DeviceReport{},
			DeviceReport{Platform: "?", Stage: "other"}},
		{"valori con contenuto estraneo", DeviceReport{Platform: "ios\nandroid", Stage: "assertion; drop table", Outcome: "failed failed"},
			DeviceReport{Platform: "?", Stage: "other", Outcome: "other"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.in.Clean()
			if got.Platform != c.want.Platform || got.Stage != c.want.Stage || got.Disposition != c.want.Disposition ||
				got.Outcome != c.want.Outcome || got.KeyPhase != c.want.KeyPhase {
				t.Fatalf("ottenuto %+v, atteso %+v", got, c.want)
			}
		})
	}
}

func TestCleanText(t *testing.T) {
	cases := []struct {
		name, in string
		limit    int
		want     string
	}{
		{"semplice", "ciao", 200, "ciao"},
		{"a capo e tabulazioni diventano spazi", "a\nb\tc\r\nd", 200, "a b c  d"},
		{"controlli ai bordi", "\x00\x1b[31mrosso\x07\n", 200, "[31mrosso"},
		{"C1 e DEL", "a\u0085b\x7fc", 200, "a b c"},
		{"separatori di riga Unicode", "a\u2028b\u2029c", 200, "a b c"},
		{"zero-width e bidi spariscono", "ab\u200bc\u202ed\u2066e", 200, "abcde"},
		{"UTF-8 non valido", "ok\xff\xfeok", 200, "ok\uFFFD\uFFFDok"},
		{"taglio a caratteri, non a byte", strings.Repeat("è", 300), 200, strings.Repeat("è", 200)},
		{"taglio esatto", strings.Repeat("a", 201), 200, strings.Repeat("a", 200)},
		{"sotto il tetto", strings.Repeat("a", 200), 200, strings.Repeat("a", 200)},
		{"spazi ai bordi", "   x  ", 200, "x"},
		{"vuoto", "", 200, ""},
		{"tetto zero", "abc", 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CleanText(c.in, c.limit)
			if got != c.want {
				t.Fatalf("CleanText(%q) = %q, atteso %q", c.in, got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("risultato non UTF-8: %q", got)
			}
		})
	}
}

func TestCleanStringsAreTruncatedAndSanitised(t *testing.T) {
	long := strings.Repeat("x", 5000) + "\n"
	in := DeviceReport{Domain: long, Detail: "riga1\nriga2\x00", Underlying: long, App: "\x1b[0m31000984", OS: long}
	got := in.Clean()
	for name, v := range map[string]string{"domain": got.Domain, "detail": got.Detail, "underlying": got.Underlying, "app": got.App, "os": got.OS} {
		if utf8.RuneCountInString(v) > MaxReportText {
			t.Errorf("%s: %d caratteri", name, utf8.RuneCountInString(v))
		}
		for _, c := range v {
			if c < 0x20 || c == 0x7f {
				t.Errorf("%s contiene il controllo %U: %q", name, c, v)
			}
		}
	}
	if got.Detail != "riga1 riga2" {
		t.Errorf("detail: %q", got.Detail)
	}
	if got.App != "[0m31000984" {
		t.Errorf("app: %q", got.App)
	}
}

func TestCleanNumbersAreBounded(t *testing.T) {
	huge := 1 << 40
	got := DeviceReport{Code: huge, Attempt: huge, KeyAgeS: huge, ElapsedMs: huge}.Clean()
	if got.Code != maxReportCode || got.Attempt != maxReportAttempt || got.KeyAgeS != maxReportKeyAge || got.ElapsedMs != maxReportElapsed {
		t.Errorf("tetti: %+v", got)
	}
	got = DeviceReport{Code: -huge, Attempt: -5, KeyAgeS: -99, ElapsedMs: -1}.Clean()
	if got.Code != -maxReportCode || got.Attempt != 0 || got.KeyAgeS != -1 || got.ElapsedMs != 0 {
		t.Errorf("minimi: %+v", got)
	}
	// i codici veri passano: NSURLErrorDomain e OSStatus sono negativi, HTTP ha tre cifre.
	for _, code := range []int{-1009, -34018, 0, 1, 3, 4, 503} {
		if got := (DeviceReport{Code: code}).Clean().Code; got != code {
			t.Errorf("code %d alterato in %d", code, got)
		}
	}
	// -1 vuol dire "ignota" e resta -1.
	if got := (DeviceReport{KeyAgeS: -1}).Clean().KeyAgeS; got != -1 {
		t.Errorf("key_age_s -1 alterato in %d", got)
	}
}

// Clean e` una funzione pura: non cambia il ricevente e applicarla due volte da`
// lo stesso risultato.
func TestCleanIsPureAndIdempotent(t *testing.T) {
	in := DeviceReport{Platform: "IOS", Stage: "boh", Detail: "a\nb", Code: 1 << 40, Disposition: "x", IOSAppOnMac: true}
	snapshot := in
	once := in.Clean()
	if !reflect.DeepEqual(in, snapshot) {
		t.Fatal("Clean ha modificato il ricevente")
	}
	if twice := once.Clean(); twice != once {
		t.Fatalf("non idempotente: %+v poi %+v", once, twice)
	}
	if !once.IOSAppOnMac {
		t.Error("ios_app_on_mac perso")
	}
}
