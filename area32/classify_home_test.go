package area32

import (
	"errors"
	"testing"
)

// Dopo il login la home senza numero di tessera vuol dire "password
// rifiutata" solo se Area32 ha rimandato al modulo di login. Qualunque altra
// pagina (manutenzione, layout cambiato) non deve contare contro il socio:
// conterebbe contro tutti.
func TestClassifyHomeWithoutCard(t *testing.T) {
	loginPage := []byte(`<script>function myFunction(){}</script><form><input type="password" id="password"></form>`)
	if err := classifyHomeWithoutCard(loginPage); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("pagina di login: %v, want ErrInvalidCredentials", err)
	}

	upper := []byte(`<INPUT TYPE="PASSWORD" NAME="pw">`)
	if err := classifyHomeWithoutCard(upper); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("pagina di login maiuscola: %v, want ErrInvalidCredentials", err)
	}

	maintenance := []byte(`<html><body><h1>Sito in manutenzione</h1></body></html>`)
	if err := classifyHomeWithoutCard(maintenance); !errors.Is(err, ErrUnexpectedPage) {
		t.Errorf("pagina sconosciuta: %v, want ErrUnexpectedPage", err)
	}
}
