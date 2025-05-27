package env

import (
	"fmt"
	"github.com/caarlos0/env/v11"
	"os"
)

type config struct {
	PasswordUUID           string `env:"PASSWORD_UUID" envDefault:"474a6581-7b01-4752-ab42-4f6539efabfe"`
	DocsUUID               string `env:"DOCS_UUID" envDefault:"f55bb831-1cbc-4af0-a243-74c974c41c36"`
	PasswordSalt           string `env:"PASSWORD_SALT" envDefault:"PROVA"`
	EmailProviderPassword  string `env:"EMAIL_PROVIDER_PASSWORD" envDefault:""`
	FirebaseAuthKey        string `env:"FIREBASE_AUTH_KEY" envDefault:""`
	StripeSecret           string `env:"STRIPE_SECRET" envDefault:""`
	StripeWebhookSignature string `env:"STRIPE_WEBHOOK_SIGNATURE" envDefault:""`
	Area32InternalEmail    string `env:"AREA32_INTERNAL_EMAIL" envDefault:""`
	Area32InternalPassword string `env:"AREA32_INTERNAL_PASSWORD" envDefault:""`
	GeminiKey              string `env:"GEMINI_KEY" envDefault:""`
	ImageRouterKey         string `env:"IMAGE_ROUTER_KEY" envDefault:""`
	GeminiResumePrompt     string `env:"GEMINI_RESUME_PROMPT" envDefault:"PARLI SOLO ITALIANO"`
	TolgeeKey              string `env:"TOLGEE_KEY" envDefault:""`
	PrintfulKey            string `env:"PRINTFUL_KEY" envDefault:""`
	PrintfulWebhookURL     string `env:"PRINTFUL_WEBHOOK_URL" envDefault:""`
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

func GetImageRouterKey() string {
	return cfg.ImageRouterKey
}
