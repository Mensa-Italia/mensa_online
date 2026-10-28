package integrity

import "sync"

// CounterWindowSize e` quanto indietro, rispetto al contatore piu` alto visto,
// puo` arrivare un'asserzione App Attest ed essere ancora accettata.
//
// Il contatore lo incrementa il Secure Enclave a ogni asserzione generata, ma
// l'app ne genera diverse in parallelo (una schermata carica eventi, timbri e
// profilo insieme) e le richieste arrivano al server in ordine sparso: con il
// controllo secco "maggiore dell'ultimo" la 2 che arriva dopo la 3 sembrava un
// replay, e un utente appena passato a "watched" vedeva fallire mezza
// schermata finche' le richieste non tornavano una alla volta.
//
// Il replay lo impedisce gia` la challenge: monouso, legata a utente, metodo e
// path, e consumata prima di verificare la firma. Il contatore resta una
// seconda difesa: dentro la finestra ogni valore passa una volta sola, fuori
// (piu` vecchio di CounterWindowSize) non passa mai.
const CounterWindowSize = 64

// CounterWindow tiene, per ogni chiave, i contatori gia` usati dentro la
// finestra. Vive in memoria: dopo un riavvio si riparte dal massimo salvato su
// DB, e le asserzioni generate prima del riavvio non hanno comunque piu` una
// challenge valida.
type CounterWindow struct {
	mu   sync.Mutex
	seen map[string]map[uint32]struct{}
}

// Accept dice se `counter` e` accettabile per la chiave `keyID`, il cui
// massimo salvato e` `stored`, e ritorna il nuovo massimo. Se accetta, segna
// il contatore come usato.
func (w *CounterWindow) Accept(keyID string, stored, counter uint32) (uint32, bool) {
	if counter == 0 {
		return stored, false // 0 e` dell'attestazione, mai di un'asserzione
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.seen == nil {
		w.seen = map[string]map[uint32]struct{}{}
	}
	used, ok := w.seen[keyID]
	if !ok {
		// Prima asserzione vista da questo processo: il massimo salvato e`
		// gia` stato usato.
		used = map[uint32]struct{}{stored: {}}
		w.seen[keyID] = used
	}

	switch {
	case counter > stored:
		used[counter] = struct{}{}
		for c := range used {
			if counter-c >= CounterWindowSize {
				delete(used, c)
			}
		}
		return counter, true
	case stored-counter >= CounterWindowSize:
		return stored, false
	}
	if _, dup := used[counter]; dup {
		return stored, false
	}
	used[counter] = struct{}{}
	return stored, true
}
