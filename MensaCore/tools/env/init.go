package env

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type config struct {
	PasswordUUID           string `env:"PASSWORD_UUID"`
	DocsUUID               string `env:"DOCS_UUID" envDefault:"f55bb831-1cbc-4af0-a243-74c974c41c36"`
	PasswordSalt           string `env:"PASSWORD_SALT"`
	EmailProviderPassword  string `env:"EMAIL_PROVIDER_PASSWORD" envDefault:""`
	FirebaseAuthKey        string `env:"FIREBASE_AUTH_KEY"`
	StripeSecret           string `env:"STRIPE_SECRET"`
	StripeWebhookSignature string `env:"STRIPE_WEBHOOK_SIGNATURE"`
	Area32InternalEmail    string `env:"AREA32_INTERNAL_EMAIL"`
	Area32InternalPassword string `env:"AREA32_INTERNAL_PASSWORD"`
	GeminiKey              string `env:"GEMINI_KEY"`
	// Modelli Gemini per testo e immagini. Stanno in env e non nel codice
	// perche` Google ritira gli id con un preavviso che non controlliamo:
	// la famiglia imagen-* e` sparita dalla Gemini API (404 NOT_FOUND su
	// models/imagen-4.0-generate-001) e ha fermato timbri e copertine AI.
	// Con questi in env il prossimo ritiro e` una variabile, non una release.
	GeminiTextModel        string `env:"GEMINI_TEXT_MODEL" envDefault:"gemini-3-flash-preview"`
	GeminiImageModel       string `env:"GEMINI_IMAGE_MODEL" envDefault:"gemini-3.1-flash-image"`
	GeminiTTSKey           string `env:"GEMINI_TTS_KEY" envDefault:""`
	GeminiTTSModel         string `env:"GEMINI_TTS_MODEL" envDefault:"gemini-3.1-flash-tts-preview"`
	GeminiTTSVoice         string `env:"GEMINI_TTS_VOICE" envDefault:"Charon"`
	GeminiTTSVoiceFemale   string `env:"GEMINI_TTS_VOICE_FEMALE" envDefault:"Zephyr"`
	GeminiTTSStylePrompt   string `env:"GEMINI_TTS_STYLE_PROMPT" envDefault:"Deep and warm tone ASMR, goosebumps"`
	GeminiTTSDirectorNote  string `env:"GEMINI_TTS_DIRECTOR_NOTE" envDefault:"Warm, understanding, soft tone with gentle inflections. Pause naturally between paragraphs."`
	GeminiTTSConcurrency   int    `env:"GEMINI_TTS_CONCURRENCY" envDefault:"2"`
	// Google Cloud Speech-to-Text v2 (per transcribe podcast in chirp_2).
	GoogleSTTCredentialsJSON string `env:"GOOGLE_STT_CREDENTIALS_JSON" envDefault:""`
	GoogleSTTProject         string `env:"GOOGLE_STT_PROJECT" envDefault:""`
	GoogleSTTLocation        string `env:"GOOGLE_STT_LOCATION" envDefault:"eu"`
	GoogleSTTEndpoint        string `env:"GOOGLE_STT_ENDPOINT" envDefault:""`
	GoogleSTTModel           string `env:"GOOGLE_STT_MODEL" envDefault:"chirp_2"`
	GoogleSTTLanguage        string `env:"GOOGLE_STT_LANGUAGE" envDefault:"it-IT"`
	GoogleSTTConcurrency     int    `env:"GOOGLE_STT_CONCURRENCY" envDefault:"4"`
	ImageRouterKey         string `env:"IMAGE_ROUTER_KEY" envDefault:""`
	GeminiResumePrompt     string `env:"GEMINI_RESUME_PROMPT" envDefault:"PARLI SOLO ITALIANO"`
	TolgeeKey              string `env:"TOLGEE_KEY" envDefault:""`
	PrintfulKey            string `env:"PRINTFUL_KEY" envDefault:""`
	PrintfulWebhookURL     string `env:"PRINTFUL_WEBHOOK_URL" envDefault:""`
	PrintfulWebhookSecret  string `env:"PRINTFUL_WEBHOOK_SECRET" envDefault:""`
	UnsplashKey            string `env:"UNSPLASH_KEY" envDefault:""`
	ZitadelPAT              string `env:"ZITADEL_PAT"`
	ZitadelHOST             string `env:"ZITADEL_HOST"`
	ZitadelOrganizationID   string `env:"ZITADEL_ORGANIZATION_ID"`
	ZitadelOIDCClientID       string `env:"ZITADEL_OIDC_CLIENT_ID"`
	ZitadelOIDCClientSecret   string `env:"ZITADEL_OIDC_CLIENT_SECRET" envDefault:""`
	ZitadelOIDCRedirectURI    string `env:"ZITADEL_OIDC_REDIRECT_URI"`
	ZitadelLoginClientUserID  string `env:"ZITADEL_LOGIN_CLIENT_USER_ID"`
	MCPClientID             string `env:"MCP_CLIENT_ID" envDefault:""`
	// Fingerprint SHA-256 aggiuntivi (separati da virgola) da pubblicare in
	// /.well-known/assetlinks.json sotto la relation get_login_creds, oltre a
	// quello della chiave di release. Servono a far funzionare le passkey sulle
	// build Android firmate col keystore di debug, che e` diverso per ogni
	// macchina di sviluppo: per questo il valore sta in env e non nel codice.
	// Lasciarlo vuoto in produzione: ogni fingerprint elencato qui puo`
	// richiedere le passkey del nostro relying party.
	AndroidDebugSHA256 string `env:"ANDROID_DEBUG_SHA256" envDefault:""`
	// Durata in secondi dei link S3 firmati che serviamo per immagini e
	// documenti. Un link firmato e` una capability anonima: chi ce l'ha
	// scarica il file senza passare piu` da noi, quindi la sua durata e` la
	// finestra in cui un link finito in un log, in un Referer o in una chat
	// resta spendibile. Stava a un'ora fissa nel codice; il default e` ora
	// cinque minuti, che basta per aprire un PDF o caricare una lista di
	// immagini. In env perche` un CDN davanti a noi puo` volere finestre piu`
	// larghe, e non si cambia una policy di sicurezza con una release.
	S3PresignTTLSeconds int `env:"S3_PRESIGN_TTL_SECONDS" envDefault:"300"`
	// FileLinkRequireAuth decide chi riceve il link S3 firmato (307) sul
	// download diretto, nell'hook OnFileDownloadRequest.
	//
	//   true  (default) — il link firmato si produce solo per una richiesta
	//                     autenticata (header Authorization). Le richieste
	//                     anonime cadono su e.Next() e il file lo serve
	//                     PocketBase, in streaming dal backend.
	//   false           — il link firmato va a chiunque, come prima di
	//                     settembre 2026: si riapre il buco (il link S3 e`
	//                     una capability anonima che circola), ma il carico
	//                     torna a essere offloadato a S3.
	//
	// ATTENZIONE a cosa NON fa. Il flag non nega mai un file: a `true` una
	// richiesta anonima NON riceve un 404, riceve gli stessi byte serviti dal
	// backend invece del redirect a S3. Quindi non evita "rotture" — nessun
	// consumatore anonimo che oggi vede un file smette di vederlo. Quello che
	// cambia e` DOVE passano i byte: a `true` ogni richiesta senza header
	// (app vecchie, crawler delle anteprime social, player audio) la serve il
	// backend invece di S3. L'unica conseguenza reale e` l'egress sul backend.
	//
	// Esiste per il cutover verso l'app che manda l'header anche sui download
	// di file (mensa_italia_app v22.2.8). Finche` la stragrande maggioranza
	// delle installazioni e` la vecchia app, che l'header non lo manda, `true`
	// significa servire dal backend quasi tutto il traffico immagini. La
	// sequenza sensata: tira su l'immagine nuova con `false` (stessa resa
	// della vecchia su chi-riceve-il-link e su offload a S3), aspetta che la
	// nuova app si diffonda sugli store, poi alza a `true`. Reversibile da
	// config, senza redeploy.
	//
	// NON e` un ripristino byte-per-byte della vecchia immagine: `false` da
	// solo tocca solo questo gate. Restano le altre modifiche della stessa
	// release, ortogonali a questo flag — il TTL del link firmato (5 min di
	// default, era 1 ora; si allunga con S3_PRESIGN_TTL_SECONDS) e l'endpoint
	// additivo GET /api/cs/file-link, che le app vecchie non chiamano.
	//
	// Il default e` `true`: un'immagine tirata su senza toccare la config e`
	// quella sicura, non quella comoda.
	FileLinkRequireAuth bool `env:"FILE_LINK_REQUIRE_AUTH" envDefault:"true"`
}

var cfg = config{}

func init() {
	if os.Getenv("DEBUG") == "true" {
		fmt.Println("DEBUG MODE ON | Getting env from .env file")
		if err := env.Parse(&cfg); err != nil {
			fmt.Printf("%+v\n", err)
		}

	} else {
		if err := env.Parse(&cfg); err != nil {
			fmt.Printf("%+v\n", err)
		}
	}
}

// MustValidate returns an error listing every missing required core env var.
// Call this at boot to fail-fast before serving traffic.
func MustValidate() error {
	var missing []string
	if cfg.PasswordUUID == "" {
		missing = append(missing, "PASSWORD_UUID")
	}
	if cfg.PasswordSalt == "" {
		missing = append(missing, "PASSWORD_SALT")
	}
	if cfg.StripeSecret == "" {
		missing = append(missing, "STRIPE_SECRET")
	}
	if cfg.FirebaseAuthKey == "" {
		missing = append(missing, "FIREBASE_AUTH_KEY")
	}
	if cfg.GeminiKey == "" {
		missing = append(missing, "GEMINI_KEY")
	}
	if cfg.ZitadelPAT == "" {
		missing = append(missing, "ZITADEL_PAT")
	}
	if cfg.ZitadelHOST == "" {
		missing = append(missing, "ZITADEL_HOST")
	}
	if cfg.ZitadelOrganizationID == "" {
		missing = append(missing, "ZITADEL_ORGANIZATION_ID")
	}
	if cfg.ZitadelOIDCClientID == "" {
		missing = append(missing, "ZITADEL_OIDC_CLIENT_ID")
	}
	if cfg.ZitadelOIDCRedirectURI == "" {
		missing = append(missing, "ZITADEL_OIDC_REDIRECT_URI")
	}
	if cfg.ZitadelLoginClientUserID == "" {
		missing = append(missing, "ZITADEL_LOGIN_CLIENT_USER_ID")
	}
	if cfg.Area32InternalEmail == "" {
		missing = append(missing, "AREA32_INTERNAL_EMAIL")
	}
	if cfg.Area32InternalPassword == "" {
		missing = append(missing, "AREA32_INTERNAL_PASSWORD")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return nil
}

func GetPasswordUUID() string {
	return cfg.PasswordUUID
}

// GetS3PresignTTL e` la durata dei link S3 firmati.
//
// Un valore non positivo in env verrebbe accettato da AWS come "gia` scaduto"
// e romperebbe ogni download senza un errore leggibile: in quel caso si torna
// al default di cinque minuti invece di fidarsi della configurazione.
func GetS3PresignTTL() time.Duration {
	if cfg.S3PresignTTLSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(cfg.S3PresignTTLSeconds) * time.Second
}

// GetFileLinkRequireAuth dice se il link S3 firmato sul download diretto va
// prodotto solo dietro autenticazione (true, sicuro e default) o per chiunque
// (false, comportamento storico per il cutover). Vedi il campo omonimo.
func GetFileLinkRequireAuth() bool {
	return cfg.FileLinkRequireAuth
}

func GetPasswordSalt() string {
	return cfg.PasswordSalt
}

func GetEmailProviderPassword() string {
	return cfg.EmailProviderPassword
}

func GetFireBaseAuthKey() string {
	return cfg.FirebaseAuthKey
}

func GetStripeSecret() string {
	return cfg.StripeSecret
}

func GetDocsUUID() string {
	return cfg.DocsUUID
}

func GetStripeWebhookSignature() string {
	return cfg.StripeWebhookSignature
}

func GetArea32InternalEmail() string {
	return cfg.Area32InternalEmail
}

func GetArea32InternalPassword() string {
	return cfg.Area32InternalPassword
}

func GetGeminiKey() string {
	return cfg.GeminiKey
}

// GetGeminiTTSKey ritorna la API key dedicata al TTS, con fallback alla
// GeminiKey condivisa se non e` impostata. Permette di isolare costi e
// rate limit del TTS dal resto delle chiamate Gemini.
func GetGeminiTTSKey() string {
	if cfg.GeminiTTSKey != "" {
		return cfg.GeminiTTSKey
	}
	return cfg.GeminiKey
}

func GetGeminiTextModel() string  { return cfg.GeminiTextModel }
func GetGeminiImageModel() string { return cfg.GeminiImageModel }

func GetGeminiTTSModel() string        { return cfg.GeminiTTSModel }
func GetGeminiTTSVoice() string        { return cfg.GeminiTTSVoice }
func GetGeminiTTSVoiceFemale() string  { return cfg.GeminiTTSVoiceFemale }
func GetGeminiTTSStylePrompt() string {
	return cfg.GeminiTTSStylePrompt
}

func GetGeminiTTSDirectorNote() string {
	return cfg.GeminiTTSDirectorNote
}

func GetGeminiTTSConcurrency() int {
	if cfg.GeminiTTSConcurrency < 1 {
		return 1
	}
	return cfg.GeminiTTSConcurrency
}

// Google Cloud Speech-to-Text v2 (chirp_2) — credentials e tuning per il
// podcast transcribe.
func GetGoogleSTTCredentialsJSON() string { return cfg.GoogleSTTCredentialsJSON }
func GetGoogleSTTProject() string         { return cfg.GoogleSTTProject }
func GetGoogleSTTLocation() string        { return cfg.GoogleSTTLocation }
func GetGoogleSTTEndpoint() string        { return cfg.GoogleSTTEndpoint }
func GetGoogleSTTModel() string           { return cfg.GoogleSTTModel }
func GetGoogleSTTLanguage() string        { return cfg.GoogleSTTLanguage }
func GetGoogleSTTConcurrency() int {
	if cfg.GoogleSTTConcurrency < 1 {
		return 1
	}
	return cfg.GoogleSTTConcurrency
}

func GetGeminiResumePrompt() string {
	return cfg.GeminiResumePrompt
}

func GetTolgeeKey() string {
	return cfg.TolgeeKey
}

func GetPrintfulKey() string {
	return cfg.PrintfulKey
}

func GetPrintfulWebhookURL() string {
	return cfg.PrintfulWebhookURL
}

func GetPrintfulWebhookSecret() string {
	return cfg.PrintfulWebhookSecret
}

func GetImageRouterKey() string {
	return cfg.ImageRouterKey
}

func GetUnsplashKey() string {
	return cfg.UnsplashKey
}

func GetZitadelPAT() string {
	return cfg.ZitadelPAT
}

func GetZitadelHost() string {
	return cfg.ZitadelHOST
}

func GetZitadelOrganizationID() string {
	return cfg.ZitadelOrganizationID
}

func GetZitadelOIDCClientID() string {
	return cfg.ZitadelOIDCClientID
}

func GetZitadelOIDCClientSecret() string {
	return cfg.ZitadelOIDCClientSecret
}

func GetZitadelOIDCRedirectURI() string {
	return cfg.ZitadelOIDCRedirectURI
}

func GetZitadelLoginClientUserID() string {
	return cfg.ZitadelLoginClientUserID
}

func GetMCPClientID() string {
	return cfg.MCPClientID
}

// GetAndroidDebugSHA256 ritorna i fingerprint di debug configurati, ripuliti e
// senza voci vuote. Nil quando la variabile non e` impostata (caso produzione).
func GetAndroidDebugSHA256() []string {
	raw := strings.Split(cfg.AndroidDebugSHA256, ",")
	out := make([]string, 0, len(raw))
	for _, f := range raw {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
