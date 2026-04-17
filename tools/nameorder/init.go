package nameorder

import (
	"sort"
	"strings"
	"unicode"
)

// OrderTokensByEmailLocalPart riordina i componenti del nome in base alla loro apparizione nella mail
func OrderTokensByEmailLocalPart(fullName, email string) []string {
	// 1) & 2) Sanitizza la email ed estrai la parte locale
	atIndex := strings.Index(email, "@")
	var localPart string
	if atIndex > -1 {
		localPart = email[:atIndex]
	} else {
		localPart = email // Fallback se non è una mail valida
	}

	// Rimuoviamo punti, trattini e accenti dalla mail per il confronto puro
	cleanEmail := sanitizeString(localPart)

	// 3) Split del fullname su space
	// Usa Fields invece di Split per gestire spazi multipli
	tokens := strings.Fields(fullName)

	// Struttura di supporto per ordinare
	type tokenData struct {
		original string
		clean    string
		index    int
	}

	var data []tokenData

	// 4) Ogni componente viene sanitizzato e identificata la sua posizione
	for _, t := range tokens {
		c := sanitizeString(t)
		idx := strings.Index(cleanEmail, c)

		// Se un token non viene trovato, gli assegniamo un indice alto (o gestiamo come preferito)
		// Qui assumiamo che facciano parte della mail come da specifiche
		if idx == -1 {
			idx = 9999
		}

		data = append(data, tokenData{
			original: t,
			clean:    c,
			index:    idx,
		})
	}

	// 5) Tutti i componenti vengono riordinati
	sort.Slice(data, func(i, j int) bool {
		return data[i].index < data[j].index
	})

	// 6) Viene ritornata la lista dei componenti ordinati
	result := make([]string, len(data))
	for i, d := range data {
		result[i] = d.original
	}

	return result
}

// sanitizeString converte in lowercase, rimuove accenti comuni e caratteri non alfanumerici
func sanitizeString(s string) string {
	s = strings.ToLower(s)

	// Mappa manuale per accenti comuni (per evitare dipendenze esterne come golang.org/x/text)
	replacer := strings.NewReplacer(
		"à", "a", "è", "e", "é", "e", "ì", "i", "ò", "o", "ù", "u",
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u",
	)
	s = replacer.Replace(s)

	// Costruiamo una stringa mantenendo solo lettere e numeri
	// Questo permette a "Marrè" (marre) di matchare con "marre.brunenghi" (marrebrunenghi)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

