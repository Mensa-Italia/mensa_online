package zauth

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"
)

// Adattamento di forma fra le WebAuthn options come le emette Zitadel e come
// le vogliono le API client.
//
// Zitadel restituisce le options cosi` come le produce go-webauthn, cioe`
// incapsulate in un envelope: {"publicKey": {...}, "mediation": "..."}.
// I client vogliono l'oggetto interno:
//
//   - Android Credential Manager: CreatePublicKeyCredentialRequest(requestJson)
//     e GetPublicKeyCredentialOption(requestJson) vogliono il JSON WebAuthn
//     senza envelope;
//   - browser: navigator.credentials.get({publicKey: <oggetto interno>});
//   - iOS: ASAuthorization non accetta JSON, decodifica campo per campo, quindi
//     gli serve comunque l'oggetto interno.
//
// Sull'encoding dei campi binari **non serve alcuna conversione**, e vale la
// pena scriverlo perche` e` il contrario di quello che si assume di solito
// (e il primo posto dove si va a cercare quando un'assertion viene rifiutata):
// go-webauthn tipizza challenge, user.id, allowCredentials[].id e
// excludeCredentials[].id come protocol.URLEncodedBase64, il cui MarshalJSON
// usa base64.RawURLEncoding. Zitadel emette quindi gia` base64url senza
// padding, che e` esattamente il formato richiesto da WebAuthn JSON. Nella
// direzione opposta lo stesso tipo fa TrimRight su '=' prima di decodificare,
// quindi i credential prodotti da Android e iOS passano senza ritocchi.
// (user.id sarebbe base64 standard con padding se Zitadel impostasse
// Config.EncodeUserIDAsString, cosa che non fa.)
const optionsEnvelopeKey = "publicKey"

// UnwrapCredentialOptions estrae l'oggetto WebAuthn interno dalle options
// restituite da Zitadel, pronto per essere passato as-is al client.
func UnwrapCredentialOptions(options *structpb.Struct) ([]byte, error) {
	if options == nil {
		return nil, errors.New("zauth: nil credential options")
	}

	inner, ok := options.GetFields()[optionsEnvelopeKey]
	if !ok {
		// Nessun envelope: Zitadel ha gia` dato l'oggetto nudo. Non e` il
		// comportamento attuale, ma non c'e` motivo di rompere se cambia.
		return options.MarshalJSON()
	}

	structVal := inner.GetStructValue()
	if structVal == nil {
		return nil, fmt.Errorf("zauth: %q is not an object", optionsEnvelopeKey)
	}
	return structVal.MarshalJSON()
}

// CredentialToStruct converte il PublicKeyCredential JSON prodotto dal client
// nella Struct che le API Zitadel accettano (VerifyPasskeyRegistration.
// PublicKeyCredential e Checks.WebAuthN.CredentialAssertionData sono entrambi
// *structpb.Struct).
//
// Il credential va passato nudo: Zitadel lo rigira a
// protocol.ParseCredential{Creation,Request}ResponseBody, che si aspettano
// l'oggetto credential e non un envelope. Se il client manda per errore
// {"publicKey": ...} il controllo su "response" lo intercetta qui, con un
// errore leggibile, invece di far fallire la parse dentro Zitadel con un 500.
func CredentialToStruct(raw []byte) (*structpb.Struct, error) {
	if len(raw) == 0 {
		return nil, errors.New("zauth: empty credential payload")
	}

	var s structpb.Struct
	if err := s.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("zauth: decode credential: %w", err)
	}

	if _, ok := s.GetFields()["response"]; !ok {
		return nil, errors.New(`zauth: credential payload missing "response"`)
	}
	return &s, nil
}
