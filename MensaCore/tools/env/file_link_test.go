package env

import "testing"

// Il default di FILE_LINK_PUBLIC_COLLECTIONS non e` una comodita`: decide cosa
// resta raggiungibile senza autenticazione quando l'immagine parte senza
// config. Sbagliarlo in un verso lascia scoperti i dati dei soci, nell'altro
// spegne in silenzio le anteprime social e i player audio — e in nessuno dei
// due casi qualcosa diventa rosso. Da qui questo test.
func TestDefaultPublicCollections(t *testing.T) {
	// Riservate: contengono dati dei soci e devono restare dietro
	// autenticazione anche senza config.
	for _, name := range []string{
		"documents", "members_registry", "users", "deals", "sigs", "events_schedule",
	} {
		if IsFileLinkPublicCollection(name) {
			t.Errorf("%q non deve essere pubblica per default", name)
		}
	}

	// Pubbliche: hanno gia` viewRule vuota nello schema, e i loro consumatori
	// un header non possono mandarlo.
	for _, name := range []string{
		"events", "stamp", "podcasts", "podcast_episodes",
		"quid_articles_audio", "ex_apps", "addons", "local_offices",
	} {
		if !IsFileLinkPublicCollection(name) {
			t.Errorf("%q deve restare pubblica per default", name)
		}
	}
}

// L'hook passa nome E id della collection: basta che uno dei due sia
// nell'elenco. Un id vuoto non deve far passare niente.
func TestPublicCollectionMatchesNameOrID(t *testing.T) {
	if !IsFileLinkPublicCollection("stamp", "pbc_123") {
		t.Error("il match sul nome deve bastare")
	}
	if IsFileLinkPublicCollection("documents", "") {
		t.Error("una collection riservata non deve passare per via dell'id vuoto")
	}
	if IsFileLinkPublicCollection("", "") {
		t.Error("nome e id vuoti non devono mai passare")
	}
}

// Il gate e` acceso per default: un'immagine tirata su senza config e` quella
// che protegge, non quella comoda.
func TestRequireAuthDefaultsOn(t *testing.T) {
	if !GetFileLinkRequireAuth() {
		t.Error("FILE_LINK_REQUIRE_AUTH deve essere true per default")
	}
}
