# scripts/

Utility a corredo non lincate dal binario `mensadb`.

## `test_zitadel_flow.sh`

Verifica end-to-end del login Zitadel + bearer su PocketBase.

```bash
bash scripts/test_zitadel_flow.sh
```

Chiede a runtime: host svc (default `https://svc.mensa.it`), email, password
(nascosta). Nessuna credenziale viene letta da file o env.

Cosa testa:

1. `POST /api/cs/auth-with-zitadel` ritorna access/refresh/id token.
2. Refresh diretto contro `auth.mensa.it/oauth/v2/token` **senza** client
   secret (l'app OIDC dev'essere "Authentication Method: None" su Zitadel).
3. Il vecchio refresh viene rifiutato dopo la rotation.
4. Batteria di ~30 GET su collezioni PB con bearer Zitadel + filtri + sort
   + `/users/auth-refresh` + `/oidc/v1/userinfo` di Zitadel.
5. Un bearer manifestamente rotto viene rifiutato dal middleware.

Stampa tempo + status di ogni chiamata e un summary `passed / failed`.

Utile dopo deploy per verificare che middleware + login client + rotation
siano allineati.
