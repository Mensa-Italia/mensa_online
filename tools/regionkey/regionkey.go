// Package regionkey e' la chiave di regione accorpata: il nome con cui le
// classifiche, le medaglie di regione e il gruppo locale riconoscono una
// regione, qualunque sia la sorgente.
//
// Tre sorgenti scrivono la regione in tre modi diversi:
//
//   - local_offices.region, il nome della sede ("Basilicata e Puglia",
//     "Piemonte e Val d'Aosta", "Emilia Romagna", ...): Of;
//   - members_registry.area, l'area del registro soci di Area32
//     ("Trentino-Alto Adige/Südtirol", "Valle d'Aosta", "Basilicata"):
//     OfRegistryArea;
//   - positions.state, la regione dell'indirizzo di eventi e convenzioni
//     ("Valle d'Aosta", "Basilicata"): OfPositionState.
//
// Le funzioni Go e le espressioni SQL (OfficeRegionSQL, RegistryAreaSQL,
// PositionStateSQL) sono la stessa mappa dei CASE di
// tools/leaderboard/sql/regioni.sql: se cambia una, cambiano tutte.
package regionkey

import "strings"

// Of ritorna la chiave di una sede (local_offices.region). Una regione che
// non e' nella mappa resta com'e'.
func Of(region string) string {
	region = strings.TrimSpace(region)
	switch region {
	case "Basilicata e Puglia":
		return "Puglia"
	case "Piemonte e Val d'Aosta":
		return "Piemonte"
	case "Emilia Romagna":
		return "Emilia-Romagna"
	case "Friuli Venezia Giulia":
		return "Friuli-Venezia Giulia"
	case "Trentino Alto Adige":
		return "Trentino-Alto Adige"
	}
	return region
}

// OfRegistryArea ritorna la chiave di un'area del registro soci
// (members_registry.area). Vuota o "NaN" vuol dire estero o sconosciuta:
// la chiave e' "".
func OfRegistryArea(area string) string {
	area = strings.TrimSpace(area)
	switch area {
	case "", "NaN":
		return ""
	case "Trentino-Alto Adige/Südtirol":
		return "Trentino-Alto Adige"
	case "Valle d'Aosta":
		return "Piemonte"
	case "Basilicata":
		return "Puglia"
	}
	return area
}

// OfPositionState ritorna la chiave della regione di un indirizzo
// (positions.state).
func OfPositionState(state string) string {
	state = strings.TrimSpace(state)
	switch state {
	case "Valle d'Aosta":
		return "Piemonte"
	case "Basilicata":
		return "Puglia"
	}
	return state
}

// OfficeRegionSQL e' Of in SQL, sulla colonna col (per esempio "lo.region").
func OfficeRegionSQL(col string) string {
	return "CASE " + col + " WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE " + col + " END"
}

// RegistryAreaSQL e' OfRegistryArea in SQL, sulla colonna col (per esempio
// "mr.area"). Le aree vuote e "NaN" vanno escluse a parte, con
// `col NOT IN (”, 'NaN')`, come nelle classifiche.
func RegistryAreaSQL(col string) string {
	return "CASE " + col + " WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE " + col + " END"
}

// PositionStateSQL e' OfPositionState in SQL, sulla colonna col (per
// esempio "p.state").
func PositionStateSQL(col string) string {
	return "CASE " + col + " WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE " + col + " END"
}
