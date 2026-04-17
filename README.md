# Mensa App Database

[![Build](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/main.yaml/badge.svg)](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/main.yaml)
[![Lint](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/lint.yaml/badge.svg)](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/lint.yaml)
[![Security](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/security.yaml/badge.svg)](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/security.yaml)
![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square&logo=go&logoColor=white)
![PocketBase](https://img.shields.io/badge/PocketBase-0.35-B8DBE4?style=flat-square)
![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker&logoColor=white)

> Backend service che alimenta l'app mobile di **Mensa Italia**. Un singolo binario Go costruito su [PocketBase](https://pocketbase.io/) che gestisce anagrafica soci, eventi, SIG, pagamenti, documenti AI, timbri, notifiche push e molto altro.

---

## Indice

- [Funzionalità](#funzionalità)
- [Architettura](#architettura)
- [Avvio locale](#avvio-locale)
- [Deploy con Docker](#deploy-con-docker)
- [API](#api)
- [Job pianificati](#job-pianificati)
- [Variabili d'ambiente](#variabili-dambiente)
- [Collezioni del database](#collezioni-del-database)

---

## Funzionalità

| | Funzionalità | Dettaglio |
|:---:|---|---|
| 👥 | **Anagrafica soci** | Sync da Area32 ogni 3 ore · snapshot giornalieri immutabili |
| 📅 | **Eventi** | Creazione, scheduling, export iCal · notifiche push su create/update |
| 🔵 | **SIG** | Gestione Special Interest Group con relazioni tra soci |
| 🏢 | **Sezioni locali** | Dati sezionali · admin · workflow accoglienza nuovi soci |
| 🏅 | **Timbri e badge** | Immagini generate da Gemini AI · verifica QR · validazione via secret |
| 📄 | **Documenti** | Fetch da Area32 · estrazione testo · riassunto AI (Gemini) · indice Zinc |
| 💳 | **Pagamenti** | Donazioni Stripe · ordini boutique · metodi di pagamento · webhook |
| 🛍️ | **E-commerce** | Integrazione Printful · lifecycle ordini via webhook |
| 🔔 | **Notifiche push** | Firebase FCM · attivate da eventi, offerte e azioni di sistema |
| 🔐 | **Autenticazione** | Zitadel OIDC · autenticazione app esterne · firma crittografica payload |
| 🎨 | **Generazione immagini** | Card evento e timbri generati da Gemini con testo dinamico |
| 🌍 | **Localizzazione** | Stringhe multilingua via Tolgee |
| 🔍 | **Ricerca full-text** | Indicizzazione documenti con Zinc Search |
| ☁️ | **Cloud storage** | CDN S3-compatible · presigned URL |
| 📆 | **Link calendario** | Feed iCal personali con accesso hash-based |

---

## Architettura

Il servizio è un **singolo binario** HTTP sulla porta `8080`. PocketBase fornisce il database SQLite embedded, l'admin UI su `/_/` e un'API REST con realtime. Tutta la logica custom è registrata all'avvio tramite hook, cron e route.

```
mensa_online/
│
├── main/
│   ├── main.go                  # Bootstrap PocketBase e registrazione route
│   ├── api/
│   │   ├── cs/                  # Core services: auth, chiavi, firma, webhook
│   │   ├── payment/             # Route Stripe
│   │   └── position/            # Stato posizioni
│   ├── crons/                   # Task schedulati in background
│   ├── hooks/                   # Hook sugli eventi dei record PocketBase
│   ├── links/                   # Redirect deep-link
│   └── utilities/               # Endpoint /.well-known
│
├── tools/
│   ├── aitools/                 # Riassunto documenti con Gemini
│   ├── aipower/                 # Generazione immagini AI (card evento, timbri)
│   ├── cdnfiles/                # Gestione file S3
│   ├── dbtools/                 # Sync DB remoto, gestione utenti
│   ├── env/                     # Caricamento tipizzato variabili d'ambiente
│   ├── notification/            # Invio notifiche push
│   ├── qrtools/                 # Generazione QR code
│   ├── signatures/              # Firma e verifica payload
│   ├── spatial/                 # Utilità geospaziali
│   ├── zauth/                   # Client OIDC Zitadel
│   └── zincsearch/              # Indicizzazione Zinc Search
│
├── area32/                      # Scraper e client API Area32
├── importers/                   # Utilità importazione dati
├── printful/                    # Integrazione API Printful
├── tolgee/                      # Servizio traduzioni
├── migrations/                  # Migrazioni schema applicate automaticamente
└── Dockerfile                   # Build multi-stage (Go → Alpine)
```

---

## Avvio locale

**Prerequisiti:** Go 1.24+

```bash
git clone https://github.com/Mensa-Italia/mensa_app_database.git
cd mensa_app_database

# Esporta le variabili d'ambiente necessarie (vedi sezione dedicata)
export GEMINI_KEY=...
export STRIPE_SECRET=...

go run main/main.go serve
```

L'admin UI è disponibile su `http://localhost:8080/_/`.
Al primo avvio con `go run`, tutte le migrazioni pendenti vengono applicate automaticamente.

---

## Deploy con Docker

**Immagine pre-compilata** da GitHub Container Registry:

```bash
docker run -p 8080:8080 \
  -v pb_data:/pb/main/pb_data \
  -e PASSWORD_UUID=<uuid> \
  -e PASSWORD_SALT=<salt> \
  ghcr.io/mensa-italia/mensa_app_database:main
```

**Con Compose** (esempio minimale):

```yaml
services:
  mensa_app_server:
    image: ghcr.io/mensa-italia/mensa_app_database:main
    ports:
      - "8080:8080"
    volumes:
      - mensa_app_server_storage:/pb/main/pb_data
    environment:
      PASSWORD_UUID: <uuid>
      PASSWORD_SALT: <salt>
      # vedi la sezione variabili d'ambiente

volumes:
  mensa_app_server_storage:
```

> Il file `compose.yaml` incluso nel repository contiene i label Traefik per HTTPS automatico.

**Build locale:**

```bash
docker build -t mensa_app_database .
```

---

## API

Tutte le route custom sono registrate sotto `/api`. L'API REST standard di PocketBase per le collezioni è disponibile su `/api/collections`.

### Pagamenti — `/api/payment`

| Metodo | Path | Descrizione |
|---|---|---|
| `POST` | `/method` | Aggiunge un metodo di pagamento Stripe |
| `GET` | `/method` | Elenca i metodi di pagamento salvati |
| `POST` | `/default` | Imposta il metodo di pagamento predefinito |
| `GET` | `/customer` | Recupera i dati del cliente Stripe |
| `POST` | `/donate` | Processa una donazione |
| `POST` | `/webhook` | Ricevitore webhook Stripe |
| `GET` | `/{id}` | Recupera un payment intent |
| `POST` | `/boutique` | Crea un pagamento boutique |
| `GET` | `/receipt/*` | Scarica/visualizza una ricevuta |

### Core services — `/api/cs`

| Metodo | Path | Descrizione |
|---|---|---|
| `POST` | `/auth-with-area` | Autenticazione tramite membership Area32 |
| `POST` | `/send-update-notify` | Invia una notifica push di aggiornamento |
| `GET` | `/force-update-addons` | Forza sync dei dati addon |
| `GET` | `/force-notification` | Forza l'invio di una notifica pendente |
| `GET` | `/force-update-state-managers` | Aggiorna i permessi utente da Area32 |
| `GET` | `/force-update-docs` | Avvia il sync dei documenti |
| `GET` | `/generate-event-card` | Genera e restituisce una card evento AI |
| `GET` | `/members-hashed` | Restituisce la lista soci in forma hash |
| `GET` | `/members-snapshots` | Elenca gli snapshot dell'anagrafica soci |
| `GET` | `/members-snapshots/{key}` | Recupera uno snapshot specifico |
| `POST` | `/keys` | Operazioni di gestione chiavi |
| `POST` | `/sign-payload` | Firma crittografica di un payload |
| `POST` | `/verify-signature` | Verifica la firma di un payload |
| `POST` | `/exapp/auth` | Autentica un'applicazione esterna |
| `POST` | `/webhook/printful` | Webhook ordini Printful |

### Posizioni — `/api/position`

| Metodo | Path | Descrizione |
|---|---|---|
| `GET` | `/state` | Recupera lo stato della posizione corrente |

### Route speciali

| Metodo | Path | Descrizione |
|---|---|---|
| `GET` | `/ical/{hash}` | Esporta il feed iCalendar personale |
| `GET` | `/static/{path...}` | Serving file statici |
| `GET` | `/force-stamp-gen/{id}` | Rigenera l'immagine di un timbro |
| `GET` | `/links/event/{id}` | Redirect deep-link a un evento |
| `GET` | `/links/stamp/{id}` | Redirect deep-link a un timbro |
| `GET` | `/.well-known/apple-app-site-association` | Universal links iOS |
| `GET` | `/.well-known/assetlinks.json` | App links Android |
| `GET` | `/.well-known/oauth-protected-resource` | Metadati OAuth 2.0 resource |
| `GET` | `/.well-known/oauth-authorization-server` | Metadati OAuth 2.0 server |
| `GET` | `/authorize` | Endpoint di autorizzazione OAuth |

---

## Job pianificati

I task vengono registrati all'avvio tramite il cron scheduler di PocketBase. Tutti gli orari sono in **UTC**.

| Schedule | Task | Descrizione |
|---|---|---|
| `1 3 * * *` | Update remote addons | Sync dati addon da Area32 |
| `1 3 * * *` | Update state manager powers | Aggiorna matrice ruoli/permessi utente |
| `1 3 * * *` | Reload Tolgee translations | Scarica le ultime stringhe di localizzazione |
| `0 6-20 * * *` | Update documents data | Sync orario documenti (Area32), solo ore lavorative |
| `30 0,3,6,9,12,15,18,21 * * *` | Update registry data | Sync anagrafica soci ogni 3 ore |
| `0 3 * * *` | Force Zitadel sync | Riconciliazione completa con l'identity provider |
| `0 0,3 * * *` | Upload files to Zinc | Re-indicizzazione documenti in Zinc Search |
| `0 */6 * * *` | Check user Stripe accounts | Validazione metodi di pagamento salvati |
| `0 3 1 * *` | Retry missing document summaries | Re-esecuzione mensile per doc senza riassunto AI |
| `0 0 * * *` | Snapshot members registry | Snapshot giornaliero immutabile dell'anagrafica |

---

## Variabili d'ambiente

### 🔐 Autenticazione e sicurezza

| Variabile | Descrizione |
|---|---|
| `PASSWORD_UUID` | Namespace UUID v5 per la derivazione delle password |
| `PASSWORD_SALT` | Salt globale per l'hashing delle password |
| `CONVERTER_TOKEN` | Token per il servizio di conversione documenti |
| `IMAGE_ROUTER_KEY` | API key per il proxy/router immagini |
| `DOCS_UUID` | Namespace UUID per l'identificazione dei documenti |
| `ZINC_USERNAME` | Username Zinc Search |
| `ZINC_PASSWORD` | Password Zinc Search |

### 📧 Email e comunicazioni

| Variabile | Descrizione |
|---|---|
| `AREA32_INTERNAL_EMAIL` | Email interna usata per le chiamate ai servizi Area32 |
| `AREA32_INTERNAL_PASSWORD` | Password per l'account interno Area32 |
| `EMAIL_PROVIDER_PASSWORD` | Password del provider SMTP |

### 🤖 AI e servizi generativi

| Variabile | Descrizione |
|---|---|
| `GEMINI_KEY` | API key Google Gemini (riassunti documenti + generazione immagini) |
| `GEMINI_RESUME_PROMPT` | System prompt per la riepilogazione documenti (default: `PARLI SOLO ITALIANO`) |
| `UNSPLASH_KEY` | API key Unsplash per le immagini degli eventi |
| `FIREBASE_AUTH_KEY` | Service account Firebase Admin SDK in JSON (raw o base64) |

### 💳 Pagamenti ed e-commerce

| Variabile | Descrizione |
|---|---|
| `STRIPE_SECRET` | Chiave segreta Stripe |
| `STRIPE_WEBHOOK_SIGNATURE` | Secret per la verifica dei webhook Stripe |
| `PRINTFUL_KEY` | API key Printful |
| `PRINTFUL_WEBHOOK_URL` | URL webhook registrato su Printful |

### 🪪 Identity provider (Zitadel)

| Variabile | Descrizione |
|---|---|
| `ZITADEL_PAT` | Personal access token Zitadel |
| `ZITADEL_HOST` | Hostname dell'istanza Zitadel |
| `ZITADEL_ORGANIZATION_ID` | ID organizzazione Zitadel |

### 🌍 Localizzazione e runtime

| Variabile | Descrizione |
|---|---|
| `TOLGEE_KEY` | API key Tolgee per il sync delle stringhe di traduzione |
| `DEBUG` | Impostare a `true` per abilitare il logging verbose |

---

## Collezioni del database

Il database SQLite embedded contiene **35 collezioni** gestite e migrate da PocketBase. Lo schema è versionato in `migrations/` e applicato automaticamente all'avvio.

| Categoria | Collezioni |
|---|---|
| **Utenti** | `users` · `users_metadata` · `users_devices` · `users_secrets` · `users_payment_method` |
| **Organizzazione** | `local_offices` · `local_offices_admins` · `local_offices_welcomes_new_members` · `positions` |
| **Soci** | `members_registry` |
| **Eventi** | `events` · `events_schedule` · `events_schedule_subscribers` |
| **Offerte** | `deals` · `deals_contacts` |
| **SIG** | `sigs` |
| **Add-on** | `addons` · `addons_private_keys` |
| **Timbri** | `stamp` · `stamp_users` · `stamp_secret` |
| **Pagamenti** | `payments` |
| **Boutique** | `boutique` · `boutique_orders` |
| **Documenti** | `documents` · `documents_elaborated` |
| **Contenuti** | `chart` · `configs` |
| **App esterne** | `ex_apps` · `ex_keys` · `ex_granted_permissions` |
| **Calendario** | `calendar_link` |
| **Notifiche** | `user_notifications` · `tickets` |
