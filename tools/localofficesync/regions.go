package localofficesync

// Mappa codice squadra (01-20) <-> nome regione, come esposto dalla pagina
// /gruppi-locali/ di mensa.it. Hardcoded perche` non cambia (le regioni
// italiane sono 20 e l'ordinamento del sito e` alfabetico stabile).
var regionsByCode = map[string]string{
	"01": "Abruzzo",
	"02": "Basilicata",
	"03": "Calabria",
	"04": "Campania",
	"05": "Emilia Romagna",
	"06": "Friuli Venezia Giulia",
	"07": "Lazio",
	"08": "Liguria",
	"09": "Lombardia",
	"10": "Marche",
	"11": "Molise",
	"12": "Piemonte",
	"13": "Puglia",
	"14": "Sardegna",
	"15": "Sicilia",
	"16": "Toscana",
	"17": "Trentino Alto Adige",
	"18": "Umbria",
	"19": "Val d'Aosta",
	"20": "Veneto",
}

// allSquadraCodes ritorna gli identificativi nell'ordine in cui le pagine
// vanno scrappate (01..20). Stabile per logging riproducibile.
func allSquadraCodes() []string {
	codes := make([]string, 0, 20)
	for i := 1; i <= 20; i++ {
		c := "00" + itoa2(i)
		codes = append(codes, c[len(c)-2:])
	}
	return codes
}

func itoa2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
