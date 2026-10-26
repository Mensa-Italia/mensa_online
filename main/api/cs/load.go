package cs

import (
	"mensadb/main/api/cs/exapp"
	"mensadb/main/api/cs/keys"
	"mensadb/main/api/cs/sign_payload"
	"mensadb/main/api/cs/verify_signature"
	"mensadb/main/api/cs/webhook"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	keys.Load(e.Group("/keys"))
	sign_payload.Load(e.Group("/sign-payload"))
	verify_signature.Load(e.Group("/verify-signature"))
	exapp.Load(e.Group("/exapp"))

	e.POST("/auth-with-area", AuthWithAreaHandler)
	e.POST("/auth-with-zitadel", AuthWithZitadelHandler)

	// Login con passkey: due passi, perche` fra il begin e il finish ci sta il
	// prompt biometrico del sistema operativo.
	e.POST("/auth-with-passkey/begin", AuthWithPasskeyBeginHandler)
	e.POST("/auth-with-passkey/finish", AuthWithPasskeyFinishHandler)

	// Gestione delle passkey dell'utente autenticato (attivazione dai settings,
	// lista, revoca).
	e.POST("/passkeys/begin", PasskeyRegisterBeginHandler)
	e.POST("/passkeys/finish", PasskeyRegisterFinishHandler)
	e.GET("/passkeys", PasskeyListHandler)
	e.DELETE("/passkeys/{id}", PasskeyDeleteHandler)

	e.GET("/me", MeHandler)

	// Link S3 firmato per un allegato, prodotto solo dietro header di
	// autenticazione. Serve dove un header non si puo` mandare: PDF aperti in
	// Safari o in un visualizzatore di sistema, fogli di condivisione, pagine
	// esterne. Vedi FileLinkHandler.
	e.GET("/file-link", FileLinkHandler)
	e.POST("/send-update-notify", SendUpdateNotifyHandler)
	e.GET("/force-update-addons", ForceUpdateAddonsHandler)
	e.GET("/force-notification", forceNotification)
	e.GET("/force-update-state-managers", ForceUpdateStateManagersHandler)
	e.GET("/force-update-docs", ForceUpdateDocsHandler)
	e.GET("/generate-event-card", GenerateEventCardHandler)
	e.GET("/members-hashed", MembersHashedHandler)
	e.GET("/members-snapshots", MembersSnapshotsHandler)
	e.GET("/members-snapshots/{key}", MemberSnapshotByKeyHandler)
	webhook.Load(e.Group("/webhook"))
}
