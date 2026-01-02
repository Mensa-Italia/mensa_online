# Mensa app DB
[![Build](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/main.yaml/badge.svg)](https://github.com/Mensa-Italia/mensa_app_database/actions/workflows/main.yaml)

This is the base repository for the Mensa app database. It contains the database schema and the data for the Mensa app.

We will use this database to handle SIG (Special Interest Group), Events, Local groups, and other data that is not handled by the main mensa servers.

## How to use
### Docker
The easiest way to run the database is to use docker.
You can copy and paste the compose.yaml file into portainer and set up all the env variables.

This will start the database and expose it on port 8080. You will have to create your own credentials for the database.

### Manual
You can also run the database manually. Just download this repo and run main/main.go with the following command
```bash
go run main/main.go serve
```

## ENV variables
The following ENV variables are required to run the database:

### 🔐 Authentication & Security

| Name | Description |
|------|-------------|
| PASSWORD_UUID | UUID namespace used for UUID v5 generation related to passwords |
| PASSWORD_SALT | Global salt used for password hashing |
| CONVERTER_TOKEN | Authentication token for the document conversion service |
| IMAGE_ROUTER_KEY | API key for the image routing / proxy service |
| DOCS_UUID | UUID namespace for document identification and versioning |
| ZINC_USERNAME | Username for accessing the Zinc service |
| ZINC_PASSWORD | Password for accessing the Zinc service |

---

### 📧 Email & Communications

| Name | Description |
|------|-------------|
| AREA32_INTERNAL_EMAIL | Internal technical email used by Area32 services |
| AREA32_INTERNAL_PASSWORD | Password associated with the internal email account |
| EMAIL_PROVIDER_PASSWORD | Password for the external SMTP / email provider |

---

### 🤖 AI & Generative Services

| Name | Description |
|------|-------------|
| GEMINI_KEY | API key for accessing Google Gemini services |
| GEMINI_RESUME_PROMPT | System prompt used for automatic document summarization |
| UNSPLASH_KEY | API key for accessing Unsplash images |
| FIREBASE_AUTH_KEY | Firebase Admin SDK service account JSON (base64 or raw) |

---

### 💳 Payments & E-commerce

| Name | Description |
|------|-------------|
| STRIPE_SECRET | Stripe secret key for server-side operations |
| STRIPE_WEBHOOK_SIGNATURE | Secret used to verify Stripe webhooks |
| PRINTFUL_KEY | API key for Printful integration |
| PRINTFUL_WEBHOOK_URL | Webhook endpoint for Printful events |

---

### 🌍 Localization & Translations

| Name | Description |
|------|-------------|
| TOLGEE_KEY | API key for the Tolgee localization service |

---

### ⚙️ System & Runtime

| Name | Description |
|------|-------------|
| PATH | System PATH used by the runtime/container |



## Notes
This doc will be updated as the project progresses. 
