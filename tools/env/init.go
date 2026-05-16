package env

import (
	"fmt"
	"os"
	"strings"

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
	ZitadelPAT             string `env:"ZITADEL_PAT"`
	ZitadelHOST            string `env:"ZITADEL_HOST"`
	ZitadelOrganizationID  string `env:"ZITADEL_ORGANIZATION_ID"`
	MCPClientID            string `env:"MCP_CLIENT_ID" envDefault:""`
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

func GetMCPClientID() string {
	return cfg.MCPClientID
}
