package integrity

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/env"
	tools "mensadb/tools/integrity"
)

const appAttestKeysCollection = "app_attest_keys"

// current e` il gate in uso: lo imposta Init, lo leggono gli endpoint.
var current atomic.Pointer[tools.Gate]

// Init costruisce il gate dalla config d'ambiente, avvia la pulizia delle
// challenge e ritorna il gate da passare a Middleware. Non fallisce mai: una
// config mancante o sbagliata non deve impedire il boot, spegne solo la
// verifica della piattaforma interessata (che per i "watched" significa
// rifiuto, cioe` fail closed) e lascia il motivo nei log.
func Init(app core.App) *tools.Gate {
	logger := app.Logger()

	challenges := tools.NewChallengeStore()
	stop := challenges.StartCleanup(30 * time.Second)
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		stop()
		return e.Next()
	})

	gate := &tools.Gate{
		Challenges: challenges,
		Sessions:   tools.NewSessionStore(),
		Decodes:    tools.NewDecodeLimiter(),
		Reports:    tools.NewUserLimiter(tools.ReportsPerHourPerUser, time.Hour),
		IOS: &tools.AppAttest{
			AppID:       env.GetAppAttestAppID(),
			Environment: env.GetAppAttestEnvironment(),
			Keys:        newPBKeyStore(app),
		},
	}

	// Android: servono SIA le credenziali del service account SIA almeno un
	// digest di certificato valido. Senza uno dei due Android resta nil e i
	// "watched" Android vengono rifiutati.
	digests, bad := tools.NormalizeCertDigests(env.GetPlayIntegrityCertSHA256())
	for _, b := range bad {
		logger.Warn("integrity: PLAY_INTEGRITY_CERT_SHA256 contiene una voce non valida, ignorata", "voce", b)
	}
	decoder := tools.NewGoogleDecoder(env.GetPlayIntegrityCredentialsJSON())
	switch {
	case decoder == nil:
		logger.Warn("integrity: PLAY_INTEGRITY_CREDENTIALS_JSON non impostata: gli utenti watched su Android verranno rifiutati (fail closed)")
	case len(digests) == 0:
		logger.Warn("integrity: PLAY_INTEGRITY_CERT_SHA256 vuota o senza voci valide: gli utenti watched su Android verranno rifiutati (fail closed)")
	default:
		gate.Android = &tools.AndroidVerifier{
			Decoder:         decoder,
			PackageName:     strings.TrimSpace(env.GetPlayIntegrityPackageName()),
			CertDigests:     digests,
			RequireLicensed: env.GetPlayIntegrityRequireLicensed(),
		}
	}

	logger.Info("integrity: inizializzato",
		"android_configurato", gate.Android != nil,
		"app_attest_app_id", gate.IOS.AppID,
		"app_attest_env", gate.IOS.Environment,
	)

	current.Store(gate)
	return gate
}
