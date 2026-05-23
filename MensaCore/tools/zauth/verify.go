package zauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mensadb/tools/env"
	"net/http"
	"strings"
	"sync"

	"github.com/zitadel/oidc/v3/pkg/client/rp"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

var (
	verifierOnce sync.Once
	verifier     *op.AccessTokenVerifier
	verifierErr  error
)

// VerifyAccessToken valida la firma (JWKS), l'issuer, exp e audience di un
// access token Zitadel. Ritorna i claim su success.
func VerifyAccessToken(ctx context.Context, token string) (*oidc.AccessTokenClaims, error) {
	v, err := getVerifier()
	if err != nil {
		return nil, err
	}
	claims, err := op.VerifyAccessToken[*oidc.AccessTokenClaims](ctx, token, v)
	if err != nil {
		return nil, err
	}
	if claims == nil {
		return nil, errors.New("zauth: nil claims")
	}
	if !audienceContains(claims.Audience, env.GetZitadelOIDCClientID()) {
		return nil, errors.New("zauth: audience mismatch")
	}
	return claims, nil
}

func getVerifier() (*op.AccessTokenVerifier, error) {
	verifierOnce.Do(func() {
		host := normalizeHTTPHost(env.GetZitadelHost())
		if host == "" {
			verifierErr = errors.New("zauth: ZITADEL_HOST not set")
			return
		}
		// Zitadel pubblica JWKS su /oauth/v2/keys.
		keySet := rp.NewRemoteKeySet(http.DefaultClient, host+"/oauth/v2/keys")
		verifier = op.NewAccessTokenVerifier(host, keySet)
	})
	return verifier, verifierErr
}

// normalizeHTTPHost garantisce uno schema https:// (se l'env contiene solo
// il dominio) e rimuove la trailing slash. Cosi` JWKS URL, authorize URL e
// confronto issuer rimangono coerenti anche se ZITADEL_HOST e` impostato
// "nudo" come "auth.mensa.it".
func normalizeHTTPHost(raw string) string {
	h := strings.TrimRight(raw, "/")
	if h == "" {
		return ""
	}
	if !strings.HasPrefix(h, "http://") && !strings.HasPrefix(h, "https://") {
		h = "https://" + h
	}
	return h
}

func audienceContains(aud []string, clientID string) bool {
	if clientID == "" {
		return true
	}
	for _, a := range aud {
		if a == clientID {
			return true
		}
	}
	return false
}

// LooksLikeZitadelJWT fa una peek sui claim del JWT (senza verifica) e
// ritorna true solo se l'issuer combacia con ZITADEL_HOST. Serve a
// distinguere i nostri token dai token PB nativi (anch'essi JWT) prima di
// pagare il costo della verifica JWKS.
func LooksLikeZitadelJWT(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// alcuni encoder usano padding standard
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return false
		}
	}
	var c struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return false
	}
	host := normalizeHTTPHost(env.GetZitadelHost())
	if host == "" || c.Issuer == "" {
		return false
	}
	return strings.TrimRight(c.Issuer, "/") == host
}
