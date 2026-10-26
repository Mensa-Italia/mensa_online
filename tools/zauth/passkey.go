package zauth

import (
	"errors"
	"fmt"
	"strings"

	sessionV2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/session/v2"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
)

// PasskeyRPID e` il relying party id delle nostre passkey.
//
// Vale auth.mensa.it, cioe` il dominio dell'istanza Zitadel, e non quello di
// questo server. La ragione non e` estetica: e` l'unico modo di condividere le
// passkey con gli altri servizi che entrano via OIDC su quello stesso IdP (per
// esempio join.svc.mensa.it). Una passkey e` legata a un rpId, quindi un rpId
// diverso significherebbe per il socio una credenziale separata solo per l'app.
//
// C'e` un secondo effetto, decisivo per le passkey gia` esistenti: quelle create
// dalla Console o dalla vecchia login UI hanno RPID vuoto, e il filtro di Zitadel
// (WebAuthNsToCredentials) le accetta solo quando l'rpId richiesto coincide con
// il dominio dell'istanza. Chiedendo auth.mensa.it funzionano senza ricrearle.
//
// Deve combaciare con il dominio che serve i due file di associazione
// app<->dominio, altrimenti iOS e Android rifiutano la cerimonia prima ancora
// di contattarci:
//   - /.well-known/apple-app-site-association (chiave "webcredentials")
//   - /.well-known/assetlinks.json (relation "get_login_creds")
//
// Zitadel non li espone: registra solo openid-configuration sotto .well-known.
// Li serve questo server (main/utilities/aasa.go e assetslinks.go), instradato
// su auth.mensa.it da una route Traefik dedicata a quei due path.
//
// Zitadel non valida questo valore: CreateWebAuthNChallenge lo gira as-is a
// go-webauthn come RPID. Il vincolo reale e` solo lato sistema operativo.
const PasskeyRPID = "auth.mensa.it"

// ErrPasskeyUnavailable copre due casi che il client deve trattare allo stesso
// modo — utente sconosciuto a Zitadel e utente senza passkey registrate — e che
// per questo vanno collassati in un unico errore.
//
// Distinguerli renderebbe l'endpoint di login un oracolo: chiunque potrebbe
// scoprire quali email sono socie e quali hanno gia` una passkey. Il chiamante
// risponde con lo stesso status in entrambi i casi, e l'app ricade sulla
// password, che e` la strada corretta per tutti e due.
var ErrPasskeyUnavailable = errors.New("zauth: no passkey available for user")

// PasskeyInfo e` la vista della passkey che esponiamo al client, senza tipi
// proto: serve alla schermata di gestione nei settings.
type PasskeyInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

// BeginPasskeyRegistration avvia la registrazione di una passkey per userID e
// ritorna l'id della passkey e le creation options gia` pronte per il client.
//
// Passa sempre per CreatePasskeyRegistrationLink con ReturnCode: e` il flusso
// previsto per un backend che agisce per conto dell'utente, e soprattutto senza
// un medium esplicito Zitadel manderebbe una mail di registrazione all'utente —
// che non e` quello che vogliamo, dato che il prompt e` gia` davanti a lui.
func BeginPasskeyRegistration(userID string) (passkeyID string, optionsJSON []byte, err error) {
	if apiClient == nil {
		return "", nil, errors.New("zauth: api client not initialized")
	}
	if strings.TrimSpace(userID) == "" {
		return "", nil, errors.New("zauth: missing userID")
	}

	linkResp, err := apiClient.UserServiceV2().CreatePasskeyRegistrationLink(ctx, &user.CreatePasskeyRegistrationLinkRequest{
		UserId: userID,
		Medium: &user.CreatePasskeyRegistrationLinkRequest_ReturnCode{
			ReturnCode: &user.ReturnPasskeyRegistrationCode{},
		},
	})
	if err != nil {
		return "", nil, fmt.Errorf("zauth: create passkey registration link: %w", err)
	}
	if linkResp.GetCode() == nil {
		return "", nil, errors.New("zauth: passkey registration link returned no code")
	}

	regResp, err := apiClient.UserServiceV2().RegisterPasskey(ctx, &user.RegisterPasskeyRequest{
		UserId:        userID,
		Code:          linkResp.GetCode(),
		Authenticator: user.PasskeyAuthenticator_PASSKEY_AUTHENTICATOR_PLATFORM,
		Domain:        PasskeyRPID,
	})
	if err != nil {
		return "", nil, fmt.Errorf("zauth: register passkey: %w", err)
	}

	optionsJSON, err = UnwrapCredentialOptions(regResp.GetPublicKeyCredentialCreationOptions())
	if err != nil {
		return "", nil, err
	}
	return regResp.GetPasskeyId(), optionsJSON, nil
}

// FinishPasskeyRegistration verifica il credential prodotto dal device e
// attiva la passkey. name e` l'etichetta mostrata nella lista dei settings.
func FinishPasskeyRegistration(userID, passkeyID, name string, credentialJSON []byte) error {
	if apiClient == nil {
		return errors.New("zauth: api client not initialized")
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(passkeyID) == "" {
		return errors.New("zauth: missing userID or passkeyID")
	}

	credential, err := CredentialToStruct(credentialJSON)
	if err != nil {
		return err
	}

	if _, err := apiClient.UserServiceV2().VerifyPasskeyRegistration(ctx, &user.VerifyPasskeyRegistrationRequest{
		UserId:              userID,
		PasskeyId:           passkeyID,
		PublicKeyCredential: credential,
		PasskeyName:         name,
	}); err != nil {
		return fmt.Errorf("zauth: verify passkey registration: %w", err)
	}
	return nil
}

// ListPasskeys ritorna le passkey dell'utente.
func ListPasskeys(userID string) ([]PasskeyInfo, error) {
	if apiClient == nil {
		return nil, errors.New("zauth: api client not initialized")
	}
	if strings.TrimSpace(userID) == "" {
		return nil, errors.New("zauth: missing userID")
	}

	resp, err := apiClient.UserServiceV2().ListPasskeys(ctx, &user.ListPasskeysRequest{UserId: userID})
	if err != nil {
		return nil, fmt.Errorf("zauth: list passkeys: %w", err)
	}

	out := make([]PasskeyInfo, 0, len(resp.GetResult()))
	for _, p := range resp.GetResult() {
		if p.GetState() == user.AuthFactorState_AUTH_FACTOR_STATE_REMOVED {
			continue
		}
		out = append(out, PasskeyInfo{
			ID:    p.GetId(),
			Name:  p.GetName(),
			Ready: p.GetState() == user.AuthFactorState_AUTH_FACTOR_STATE_READY,
		})
	}
	return out, nil
}

// RemovePasskey revoca una passkey. Non e` un'operazione rischiosa: la password
// resta sempre un percorso di accesso valido, quindi non c'e` modo di chiudersi
// fuori dall'account rimuovendo l'ultima passkey.
func RemovePasskey(userID, passkeyID string) error {
	if apiClient == nil {
		return errors.New("zauth: api client not initialized")
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(passkeyID) == "" {
		return errors.New("zauth: missing userID or passkeyID")
	}

	if _, err := apiClient.UserServiceV2().RemovePasskey(ctx, &user.RemovePasskeyRequest{
		UserId:    userID,
		PasskeyId: passkeyID,
	}); err != nil {
		return fmt.Errorf("zauth: remove passkey: %w", err)
	}
	return nil
}

// BeginPasskeyLogin apre una sessione Zitadel con il check utente soddisfatto e
// una challenge WebAuthn pendente, e ritorna le request options per il client.
//
// USER_VERIFICATION_REQUIREMENT_REQUIRED non e` una preferenza estetica: e` cio`
// che fa leggere a Zitadel i token *passwordless* dell'utente invece di quelli
// U2F (vedi getHumanWebAuthNTokenReadModel upstream). Con qualunque altro valore
// la challenge verrebbe costruita sul set di credenziali sbagliato.
//
// Il sessionToken ritornato non va mai consegnato al client: e` un bearer verso
// le API Zitadel. Il chiamante lo tiene server-side e da` al client solo un
// riferimento opaco.
func BeginPasskeyLogin(loginName string) (sessionID, sessionToken string, optionsJSON []byte, err error) {
	if apiClient == nil {
		return "", "", nil, errors.New("zauth: api client not initialized")
	}
	if strings.TrimSpace(loginName) == "" {
		return "", "", nil, ErrPasskeyUnavailable
	}

	// Nessun pre-check di esistenza dell'utente: sarebbe un round-trip in piu`
	// e renderebbe la latenza diversa fra "utente ignoto" e "utente senza
	// passkey", che e` esattamente la differenza che non vogliamo far osservare.
	resp, err := apiClient.SessionServiceV2().CreateSession(ctx, &sessionV2.CreateSessionRequest{
		Checks: &sessionV2.Checks{
			User: &sessionV2.CheckUser{
				Search: &sessionV2.CheckUser_LoginName{LoginName: loginName},
			},
		},
		Challenges: &sessionV2.RequestChallenges{
			WebAuthN: &sessionV2.RequestChallenges_WebAuthN{
				Domain:                      PasskeyRPID,
				UserVerificationRequirement: sessionV2.UserVerificationRequirement_USER_VERIFICATION_REQUIREMENT_REQUIRED,
			},
		},
	})
	if err != nil {
		if isUserNotFoundError(err) || isNoWebAuthNCredentialsError(err) {
			return "", "", nil, ErrPasskeyUnavailable
		}
		return "", "", nil, fmt.Errorf("zauth: create passkey session: %w", err)
	}

	options := resp.GetChallenges().GetWebAuthN().GetPublicKeyCredentialRequestOptions()
	if options == nil {
		// Sessione creata ma nessuna challenge: in pratica significa che non
		// c'era niente su cui sfidare l'utente.
		return "", "", nil, ErrPasskeyUnavailable
	}

	optionsJSON, err = UnwrapCredentialOptions(options)
	if err != nil {
		return "", "", nil, err
	}
	return resp.GetSessionId(), resp.GetSessionToken(), optionsJSON, nil
}

// FinishPasskeyLogin verifica l'assertion contro la challenge pendente e
// converte la sessione in token OIDC.
func FinishPasskeyLogin(sessionID, sessionToken string, credentialJSON []byte) (*TokenSet, error) {
	if apiClient == nil {
		return nil, errors.New("zauth: api client not initialized")
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(sessionToken) == "" {
		return nil, errors.New("zauth: missing session reference")
	}

	assertion, err := CredentialToStruct(credentialJSON)
	if err != nil {
		return nil, err
	}

	resp, err := apiClient.SessionServiceV2().SetSession(ctx, &sessionV2.SetSessionRequest{
		SessionId:    sessionID,
		SessionToken: sessionToken,
		Checks: &sessionV2.Checks{
			WebAuthN: &sessionV2.CheckWebAuthN{
				CredentialAssertionData: assertion,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("zauth: set session webauthn: %w", err)
	}

	// SetSession ruota il token di sessione: proseguire con quello vecchio fa
	// fallire CreateCallback. Se la risposta non ne porta uno nuovo teniamo il
	// precedente, che resta valido.
	nextToken := resp.GetSessionToken()
	if nextToken == "" {
		nextToken = sessionToken
	}
	return TokensForSession(sessionID, nextToken)
}

// isNoWebAuthNCredentialsError riconosce il caso "l'utente esiste ma non ha
// passkey". go-webauthn fallisce BeginLogin con "Found no credentials for user",
// che Zitadel avvolge in WEBAU-4G8sw / Errors.User.WebAuthN.BeginLoginFailed.
// Match su tutte e tre le forme perche` quella che arriva al client dipende da
// come l'istanza traduce i messaggi.
func isNoWebAuthNCredentialsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "webau-4g8sw") ||
		strings.Contains(msg, "beginloginfailed") ||
		strings.Contains(msg, "found no credentials")
}
