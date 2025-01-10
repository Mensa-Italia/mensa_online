package env

import (
	"fmt"
	"github.com/caarlos0/env/v11"
)

type config struct {
	PasswordUUID          string `env:"PASSWORD_UUID" envDefault:"474a6581-7b01-4752-ab42-4f6539efabfe"`
	PasswordSalt          string `env:"PASSWORD_SALT" envDefault:"PROVA"`
	EmailProviderPassword string `env:"EMAIL_PROVIDER_PASSWORD" envDefault:""`
	FirebaseAuthKey       string `env:"FIREBASE_AUTH_KEY" envDefault:""`
	StripeSecret          string `env:"STRIPE_SECRET" envDefault:""`
}

var cfg = config{}

func init() {
	if err := env.Parse(&cfg); err != nil {
		fmt.Printf("%+v\n", err)
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
