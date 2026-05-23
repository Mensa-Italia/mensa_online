package spatial

import "testing"

func TestLoadState(t *testing.T) {
	cases := []struct {
		name     string
		lat, lon float64
		want     string
	}{
		{"Roma", 41.9028, 12.4964, "Lazio"},
		{"Milano", 45.4642, 9.1900, "Lombardia"},
		{"Napoli", 40.8518, 14.2681, "Campania"},
		{"Palermo", 38.1157, 13.3613, "Sicilia"},
		{"Cagliari", 39.2238, 9.1217, "Sardegna"},
		{"Trento", 46.0667, 11.1167, "Trentino-Alto Adige"},
		{"Aosta", 45.7372, 7.3153, "Valle d'Aosta"},
		{"FuoriItalia_Parigi", 48.8566, 2.3522, "NaN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LoadState(c.lat, c.lon); got != c.want {
				t.Errorf("LoadState(%v, %v) = %q, want %q", c.lat, c.lon, got, c.want)
			}
		})
	}
}
