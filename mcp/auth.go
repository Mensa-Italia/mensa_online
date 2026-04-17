package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc"
	jwt "github.com/golang-jwt/jwt/v4"
	"mensadb/tools/env"
)

const (
	oidcIssuer = "https://auth.mensa.it"
	jwksURL    = "https://auth.mensa.it/oauth/v2/keys"
)

// contextKey is an unexported type to avoid context key collisions.
type contextKey string

const claimsKey contextKey = "mcp_claims"

// Claims wraps the standard JWT registered claims.
// azp (authorized party) is the OAuth2 client ID that requested the token —
// present in Zitadel access tokens alongside the aud array.
type Claims struct {
	jwt.RegisteredClaims
	AuthorizedParty string `json:"azp"`
}

// validMethods mirrors the algorithms advertised by auth.mensa.it.
var validMethods = []string{"RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"}

// jwks is initialised exactly once at first use and then kept alive by a
// background goroutine that refreshes the key set from the JWKS endpoint.
var (
	jwksOnce sync.Once
	jwksInst *keyfunc.JWKS
	jwksErr  error
)

func getJWKS() (*keyfunc.JWKS, error) {
	jwksOnce.Do(func() {
		jwksInst, jwksErr = keyfunc.Get(jwksURL, keyfunc.Options{
			// Proactively refresh keys every hour.
			RefreshInterval: time.Hour,
			// Also refresh whenever we see an unknown kid, but throttle to
			// avoid hammering the server if a bad token is sent repeatedly.
			RefreshUnknownKID: true,
			RefreshRateLimit:  5 * time.Minute,
		})
	})
	return jwksInst, jwksErr
}

// newAuthMiddleware returns an http.Handler that validates the Bearer token
// found in the Authorization header against auth.mensa.it before forwarding
// the request. The parsed *Claims are stored in the context and can be
// retrieved with ClaimsFromContext.
func newAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawToken, err := extractBearer(r)
		if err != nil {
			setWWWAuthenticate(w, r)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		jwks, err := getJWKS()
		if err != nil {
			http.Error(w, "auth service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(rawToken, claims, jwks.Keyfunc,
			jwt.WithValidMethods(validMethods),
		)
		if err != nil || !token.Valid {
			setWWWAuthenticate(w, r)
			http.Error(w, fmt.Sprintf("invalid token: %v", err), http.StatusUnauthorized)
			return
		}

		if claims.Issuer != oidcIssuer {
			setWWWAuthenticate(w, r)
			http.Error(w, "token issuer not accepted", http.StatusUnauthorized)
			return
		}

		// Client ID check: if MCP_CLIENT_ID is set, the token must have that
		// value in either the aud array or the azp claim.
		if clientID := env.GetMCPClientID(); clientID != "" {
			inAud := claims.VerifyAudience(clientID, false)
			inAzp := claims.AuthorizedParty == clientID
			if !inAud && !inAzp {
				setWWWAuthenticate(w, r)
				http.Error(w, "token not authorized for this service", http.StatusUnauthorized)
				return
			}
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// setWWWAuthenticate aggiunge il header WWW-Authenticate alla risposta.
// Il campo resource_metadata punta al well-known endpoint su questo stesso
// host, così i client MCP (es. Claude Web) sanno dove trovare il metadata
// dell'authorization server senza fare fallback su {origin}/authorize.
func setWWWAuthenticate(w http.ResponseWriter, r *http.Request) {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	metaURL := fmt.Sprintf("%s://%s/.well-known/oauth-protected-resource", scheme, r.Host)
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s"`, metaURL))
}

// ClaimsFromContext retrieves the validated *Claims injected by the auth
// middleware. Returns (nil, false) if the context carries no claims.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

func extractBearer(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", fmt.Errorf("authorization header missing")
	}
	if !strings.HasPrefix(h, "Bearer ") {
		return "", fmt.Errorf("authorization header must use Bearer scheme")
	}
	tok := strings.TrimPrefix(h, "Bearer ")
	if tok == "" {
		return "", fmt.Errorf("bearer token is empty")
	}
	return tok, nil
}
