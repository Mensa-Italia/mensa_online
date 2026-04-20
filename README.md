<h1 align="center">Mensa Online</h1>

<p align="center">
  <em>Backend Go + PocketBase che alimenta l'infrastruttura digitale di Mensa Italia.</em>
</p>

<p align="center">
  <a href="https://github.com/Mensa-Italia/mensa_online/actions/workflows/lint.yaml"><img alt="Lint" src="https://github.com/Mensa-Italia/mensa_online/actions/workflows/lint.yaml/badge.svg"></a>
  <a href="https://github.com/Mensa-Italia/mensa_online/actions/workflows/security.yaml"><img alt="Security" src="https://github.com/Mensa-Italia/mensa_online/actions/workflows/security.yaml/badge.svg"></a>
  <a href="https://github.com/Mensa-Italia/mensa_online/actions/workflows/gitleaks.yaml"><img alt="Gitleaks" src="https://github.com/Mensa-Italia/mensa_online/actions/workflows/gitleaks.yaml/badge.svg"></a>
  <a href="https://github.com/Mensa-Italia/mensa_online/actions/workflows/main.yaml"><img alt="Build" src="https://github.com/Mensa-Italia/mensa_online/actions/workflows/main.yaml/badge.svg"></a>
</p>

<p align="center">
  <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/Go-1.25.9-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://pocketbase.io/"><img alt="PocketBase" src="https://img.shields.io/badge/PocketBase-0.35-B8DBE4"></a>
  <a href="https://www.docker.com/"><img alt="Docker" src="https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white"></a>
  <a href="https://github.com/Mensa-Italia/mensa_online/pkgs/container/mensa_online"><img alt="ghcr" src="https://img.shields.io/badge/ghcr.io-mensa__online-181717?logo=github"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-GPL--2.0-green"></a>
</p>

---

## Panoramica

**Mensa Online** è il backend che alimenta l'app mobile ufficiale di Mensa Italia e i servizi digitali dell'associazione. Un singolo binario Go costruito su [PocketBase](https://pocketbase.io/) che espone API REST + realtime, un admin UI integrato e tutta la logica custom registrata come hook, cron e route HTTP.

Il servizio in produzione è raggiungibile su `https://svc.mensa.it`. Il client mobile è pubblicato in [Mensa-Italia/mensa_italia_app](https://github.com/Mensa-Italia/mensa_italia_app).

## Funzionalità

- **Anagrafica soci** — sync da Area32 ogni 3 ore, snapshot giornalieri immutabili
- **Eventi** — creazione, scheduling, export iCal, notifiche push su create/update
- **SIG** — gestione Special Interest Group con relazioni tra soci
- **Sezioni locali** — dati sezionali, admin, workflow accoglienza nuovi soci
- **Timbri e badge** — immagini generate da Gemini AI, verifica QR, validazione via secret
- **Documenti** — fetch da Area32, estrazione testo, riassunto AI (Gemini), indice Zinc
- **Pagamenti** — donazioni Stripe, ordini boutique, metodi di pagamento, webhook firmati
- **E-commerce Printful** — catalogo + lifecycle ordini via webhook
- **Notifiche push** — Firebase FCM attivate da eventi, offerte e azioni di sistema
- **Autenticazione** — Zitadel OIDC, fallback Area32, firma crittografica payload
- **Generazione immagini AI** — card evento e timbri con testo dinamico via Gemini
- **Localizzazione** — stringhe multilingua via Tolgee
- **Ricerca full-text** — indicizzazione documenti con Zinc Search
- **Cloud storage** — CDN S3-compatible con presigned URL
- **Link calendario** — feed iCal personali con accesso hash-based
- **MCP server** — esposizione tool su `/mcp` protetti da OAuth 2.0

## Stack tecnologico

| Categoria | Tecnologia |
|---|---|
| Linguaggio | Go 1.25.9 |
| Framework | [PocketBase 0.35](https://pocketbase.io/) (SQLite embedded) |
| HTTP | Router integrato PocketBase + `net/http` |
| Migrations | `migratecmd` (automigrate in dev, JSON baseline + Go files) |
| Identity | [Zitadel OIDC](https://zitadel.com/) · Area32 legacy scraper |
| Pagamenti | [Stripe Go SDK v81](https://github.com/stripe/stripe-go) |
| E-commerce | [Printful API](https://developers.printful.com/) |
| AI | [Google Gemini](https://ai.google.dev/) (testo + immagini) via `google.golang.org/genai` |
| Push notifications | [Firebase Admin SDK](https://firebase.google.com/docs/admin/setup) |
| i18n | [Tolgee](https://tolgee.io/) |
| Full-text search | [Zinc Search](https://zincsearch-docs.zinc.dev/) |
| Storage | AWS S3 SDK v2 (presigned URL) |
| Images | [Unsplash](https://unsplash.com/developers) · Image Router proxy |
| PDF | `pdfcpu` + Ghostscript nel container |
| Calendar | `arran4/golang-ical` |
| QR codes | `yeqown/go-qrcode` |
| MCP | [mark3labs/mcp-go](https://github.com/mark3labs/mcp-go) |
| Container | Docker multi-stage (Alpine) |
| Registry | `ghcr.io/mensa-italia/mensa_app_database` |
| Reverse proxy | [Traefik](https://traefik.io/) (HTTPS auto) |
| CI | GitHub Actions (lint · security · gitleaks · build · update) |

## Prerequisiti

- [Go 1.25.9+](https://go.dev/dl/) (versione pinnata in `go.mod`)
- [Docker](https://www.docker.com/) (opzionale, per build container e deploy)
- Accesso alle chiavi dei servizi esterni usati nei flussi completi (Gemini, Stripe, Firebase, Printful, Zitadel, Tolgee, Zinc, S3, Area32, Unsplash). Per lo sviluppo locale basta un sottoinsieme — vedi la [Configuration wiki](https://github.com/Mensa-Italia/mensa_online/wiki/Configuration).

## Setup

```bash
# 1. Clone
git clone https://github.com/Mensa-Italia/mensa_online.git
cd mensa_online

# 2. Esporta (o imposta via .env con DEBUG=true) le variabili minime
export PASSWORD_UUID=<uuid>
export PASSWORD_SALT=<salt>
# + chiavi dei servizi che vuoi testare: GEMINI_KEY, STRIPE_SECRET, ...

# 3. Avvio in sviluppo (automigrate attivo)
go run main/main.go serve
```

- Admin UI: [http://localhost:8080/_/](http://localhost:8080/_/)
- API REST collezioni: `http://localhost:8080/api/collections/*`
- API custom: `http://localhost:8080/api/*`

Al primo avvio con `go run` tutte le migrazioni pendenti vengono applicate automaticamente.

### Build & test

```bash
# Build binario
go build ./...

# Vet + lint
go vet ./...
golangci-lint run --timeout=5m
```

## Deploy con Docker

**Immagine pre-compilata** da GitHub Container Registry:

```bash
docker run -p 8080:8080 \
  -v pb_data:/pb/main/pb_data \
  -e PASSWORD_UUID=<uuid> \
  -e PASSWORD_SALT=<salt> \
  ghcr.io/mensa-italia/mensa_app_database:main
```

**Compose** (esempio minimale, produzione usa Traefik come in `compose.yaml`):

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
      # + tutte le altre chiavi dei servizi esterni

volumes:
  mensa_app_server_storage:
```

**Build locale:**

```bash
docker build -t mensa_app_database .
```

## Architettura

```
┌─────────────────────────────────────────────────────────────────┐
│                    PocketBase (SQLite + Admin UI)                │
│                                                                   │
│   HTTP Router :8080   →   /api/collections/*  (CRUD)             │
│                       →   /api/*             (custom handlers)   │
│                       →   /_/                (admin UI)          │
│                       →   /mcp               (MCP + OAuth)       │
│                       →   /.well-known/*     (OIDC + AASA)       │
│                                                                   │
│   Hooks   (record lifecycle)    Cron scheduler   (scheduled jobs)│
└──────────────┬──────────────────────────────────────────────────┘
               │
   ┌───────────┼───────────┬───────────┬───────────┬───────────┐
   ▼           ▼           ▼           ▼           ▼           ▼
 Stripe     Printful    Gemini     Firebase    Zitadel    Area32
(payments) (e-commerce) (AI)       (push)     (OIDC)    (legacy)
                                                              │
                                           ┌──────────────────┴─┐
                                           ▼                    ▼
                                        Tolgee            Zinc Search
                                         (i18n)          (full-text)
                                                              │
                                                              ▼
                                                        S3 / CDN
```

Per un'analisi completa con diagrammi mermaid vedi [Architecture Overview](https://github.com/Mensa-Italia/mensa_online/wiki/Architecture-Overview).

## Struttura del progetto

```
mensa_online/
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
├── mcp/                         # Server MCP (Model Context Protocol) su /mcp
│
├── tools/
│   ├── aitools/                 # Riassunto documenti con Gemini
│   ├── aipower/                 # Generazione immagini AI (card evento, timbri)
│   ├── cdnfiles/                # Gestione file S3 + presigned URL
│   ├── dbtools/                 # Sync DB remoto, gestione utenti
│   ├── env/                     # Caricamento tipizzato variabili d'ambiente
│   ├── notification/            # Invio notifiche push (Firebase FCM)
│   ├── qrtools/                 # Generazione QR code
│   ├── signatures/              # Firma e verifica payload
│   ├── spatial/                 # Utilità geospaziali
│   ├── zauth/                   # Client OIDC Zitadel
│   └── zincsearch/              # Indicizzazione Zinc Search
│
├── area32/                      # Scraper e client API Area32 (identity legacy)
├── importers/                   # Utilità importazione dati
├── printful/                    # Integrazione API Printful
├── tolgee/                      # Servizio traduzioni
├── migrations/                  # Schema versionato, applicato automaticamente
├── pb_public/                   # File statici serviti su /static
├── Dockerfile                   # Build multi-stage (Go → Alpine)
├── compose.yaml                 # Stack produzione con label Traefik
└── go.mod                       # Dipendenze Go (module: mensadb)
```

## API

Route principali:

| Gruppo | Base path | Contenuto |
|---|---|---|
| Pagamenti | `/api/payment` | Metodi, donazioni, boutique, webhook Stripe, ricevute |
| Core services | `/api/cs` | Auth Area32, notifiche, chiavi, firma/verifica, exapp, webhook Printful |
| Posizioni | `/api/position` | Stato della posizione corrente |
| Collezioni | `/api/collections/*` | API REST PocketBase standard |
| Link | `/links/event/{id}` · `/links/stamp/{id}` | Redirect deep-link |
| iCal | `/ical/{hash}` | Feed iCalendar personale |
| OIDC | `/.well-known/oauth-*` · `/authorize` | Metadati OAuth 2.0 + MCP |
| App links | `/.well-known/apple-app-site-association` · `/assetlinks.json` | Universal links iOS/Android |
| MCP | `/mcp` | MCP streamable HTTP server (tool use) |

Il riferimento completo, parametri e auth required sono nella [API Reference wiki](https://github.com/Mensa-Italia/mensa_online/wiki/API-Reference).

## Job pianificati

Tutti gli orari sono in **UTC**. Registrati via `app.Cron().MustAdd(...)` all'avvio.

| Schedule | Task |
|---|---|
| `1 3 * * *` | Update remote addons · state manager powers · Tolgee reload |
| `0 6-20 * * *` | Update documents data da Area32 (ore lavorative) |
| `30 0,3,6,9,12,15,18,21 * * *` | Update registry soci ogni 3 ore |
| `0 3 * * *` | Force Zitadel sync |
| `0 0,3 * * *` | Upload file a Zinc Search |
| `0 */6 * * *` | Check Stripe accounts utenti |
| `0 3 1 * *` | Retry mensile documenti senza riassunto |
| `0 0 * * *` | Snapshot giornaliero anagrafica soci |

Dettagli in [Background Jobs & Hooks](https://github.com/Mensa-Italia/mensa_online/wiki/Background-Jobs-and-Hooks).

## Documentazione

La documentazione tecnica completa è mantenuta nella [**Wiki del repository**](https://github.com/Mensa-Italia/mensa_online/wiki):

- [Getting Started](https://github.com/Mensa-Italia/mensa_online/wiki/Getting-Started) — setup esteso, prerequisiti, primo avvio
- [Architecture Overview](https://github.com/Mensa-Italia/mensa_online/wiki/Architecture-Overview) — diagrammi, componenti, flusso richieste
- [Configuration](https://github.com/Mensa-Italia/mensa_online/wiki/Configuration) — variabili d'ambiente, PocketBase, secrets
- [API Reference](https://github.com/Mensa-Italia/mensa_online/wiki/API-Reference) — endpoint, parametri, auth
- [Database Schema](https://github.com/Mensa-Italia/mensa_online/wiki/Database-Schema) — 35 collezioni, viste, ER diagram
- [Authentication & Authorization](https://github.com/Mensa-Italia/mensa_online/wiki/Authentication-and-Authorization)
- [Background Jobs & Hooks](https://github.com/Mensa-Italia/mensa_online/wiki/Background-Jobs-and-Hooks)
- [External Integrations](https://github.com/Mensa-Italia/mensa_online/wiki/External-Integrations)
- [Deployment](https://github.com/Mensa-Italia/mensa_online/wiki/Deployment) — Docker, CI/CD, Traefik
- [Troubleshooting & FAQ](https://github.com/Mensa-Italia/mensa_online/wiki/Troubleshooting-and-FAQ)
- [Contributing Guidelines](https://github.com/Mensa-Italia/mensa_online/wiki/Contributing-Guidelines)

## Contributing

I contributi sono benvenuti. Prima di aprire una Pull Request leggi le [Contributing Guidelines](https://github.com/Mensa-Italia/mensa_online/wiki/Contributing-Guidelines) complete. In sintesi:

1. Fork del repository e branch dedicato dal ramo `dev` (`feature/xxx` o `fix/xxx`)
2. `go build ./...` + `go vet ./...` devono essere puliti
3. `golangci-lint run --timeout=5m` passa senza warning
4. Nessun segreto committato (il workflow Gitleaks blocca i PR)
5. Se aggiungi una nuova variabile d'ambiente: aggiorna `tools/env/init.go` + la pagina [Configuration](https://github.com/Mensa-Italia/mensa_online/wiki/Configuration)
6. Se modifichi lo schema: aggiungi una migrazione in `migrations/<timestamp>_<nome>.go` e verifica che l'automigrate funzioni
7. Commit in italiano con prefisso conventional (`feat:`, `fix:`, `chore:`, `refactor:`, `docs:`)
8. Apri la PR contro `dev` con descrizione dettagliata

Per segnalare bug apri una [issue](https://github.com/Mensa-Italia/mensa_online/issues). Per vulnerabilità di sicurezza utilizza [GitHub Security Advisories](https://github.com/Mensa-Italia/mensa_online/security/advisories) — **non** aprire issue pubbliche.

## License

Distribuito sotto licenza **GNU General Public License v2.0**. Vedi [`LICENSE`](LICENSE) per il testo completo.

## Link utili

- Sito ufficiale: [mensa.it](https://www.mensa.it)
- App mobile: [mensa_italia_app](https://github.com/Mensa-Italia/mensa_italia_app)
- Wiki: [github.com/Mensa-Italia/mensa_online/wiki](https://github.com/Mensa-Italia/mensa_online/wiki)
- Issue tracker: [github.com/Mensa-Italia/mensa_online/issues](https://github.com/Mensa-Italia/mensa_online/issues)
- Container image: [`ghcr.io/mensa-italia/mensa_app_database`](https://github.com/Mensa-Italia/mensa_online/pkgs/container/mensa_online)
- PocketBase docs: [pocketbase.io/docs](https://pocketbase.io/docs/)
