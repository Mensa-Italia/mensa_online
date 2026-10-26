package main

import (
	"net/http"
	"net/netip"

	"github.com/pocketbase/pocketbase/core"
)

// La dashboard di PocketBase (/_/) carica immagini e documenti con un
// semplice <img src="/api/files/..."> e aggiunge un file token in ?token= solo
// per le collection che hanno un campo file `protected` (qui: deals). Per
// tutte le altre la richiesta parte senza header e senza token, e l'hook
// OnFileDownloadRequest risponde 404 anche a un superuser loggato.
//
// La dashboard non la possiamo cambiare, ma quelle richieste sono same-origin e
// portano i cookie. Quando un superuser si autentica (login o auth-refresh, che
// la dashboard fa a ogni apertura) gli lasciamo il token di auth in un cookie
// HttpOnly visibile solo sotto /api/files/; il download lo accetta solo se il
// token e` valido e appartiene a _superusers.
const (
	superuserFileCookieName = "pb_superuser_files"
	superuserFileCookiePath = "/api/files/"
)

// bindSuperuserFileCookie imposta il cookie sulle risposte di auth dei
// superuser andate a buon fine.
//
// Il cookie si aggiunge quando la risposta scrive lo status, e solo se e` 200:
// l'hook gira prima del controllo MFA, e con l'MFA attiva il primo fattore
// risponde 401 con un mfaId; li` il token dell'evento non deve uscire.
func bindSuperuserFileCookie(app core.App) {
	app.OnRecordAuthRequest(core.CollectionNameSuperusers).BindFunc(func(e *core.RecordAuthRequestEvent) error {
		e.Response = &cookieOnSuccessWriter{
			ResponseWriter: e.Response,
			cookie: &http.Cookie{
				Name:     superuserFileCookieName,
				Value:    e.Token,
				Path:     superuserFileCookiePath,
				MaxAge:   int(e.Collection.AuthToken.DurationTime().Seconds()),
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteStrictMode,
			},
		}
		return e.Next()
	})
}

// isSuperuserFileCookieValid dice se la richiesta porta il cookie di un
// superuser con un token di auth ancora valido. Come fa PocketBase per i file
// token dei superuser, vale anche la whitelist degli IP (Settings ->
// Superuser IPs) quando e` impostata.
func isSuperuserFileCookieValid(e *core.FileDownloadRequestEvent) bool {
	cookie, err := e.Request.Cookie(superuserFileCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}

	record, err := e.App.FindAuthRecordByToken(cookie.Value, core.TokenTypeAuth)
	if err != nil || record == nil || !record.IsSuperuser() {
		return false
	}

	allowedIPs := e.App.Settings().SuperuserIPs
	return len(allowedIPs) == 0 || isIPInList(allowedIPs, e.RealIP())
}

// isIPInList replica la funzione omonima non esportata di PocketBase
// (apis/middlewares_rate_limit.go): ogni voce e` un IP o una subnet CIDR.
func isIPInList(ipsOrSubnets []string, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}

	for _, item := range ipsOrSubnets {
		if prefix, err := netip.ParsePrefix(item); err == nil {
			if prefix.Contains(addr) {
				return true
			}
			continue
		}
		if itemAddr, err := netip.ParseAddr(item); err == nil && itemAddr == addr {
			return true
		}
	}

	return false
}

// cookieOnSuccessWriter aggiunge il cookie al momento di scrivere lo status, e
// solo se lo status e` 200. Unwrap permette al router di PocketBase di
// arrivare al writer originale (Written, Status, Flush).
type cookieOnSuccessWriter struct {
	http.ResponseWriter
	cookie      *http.Cookie
	wroteHeader bool
}

func (w *cookieOnSuccessWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		if status == http.StatusOK {
			http.SetCookie(w.ResponseWriter, w.cookie)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *cookieOnSuccessWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *cookieOnSuccessWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
