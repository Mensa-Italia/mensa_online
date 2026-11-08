package regionkey

import "testing"

func TestMappings(t *testing.T) {
	cases := []struct {
		fn       func(string) string
		in, want string
	}{
		{Of, "Basilicata e Puglia", "Puglia"},
		{Of, "Piemonte e Val d'Aosta", "Piemonte"},
		{Of, "Emilia Romagna", "Emilia-Romagna"},
		{Of, "Friuli Venezia Giulia", "Friuli-Venezia Giulia"},
		{Of, "Trentino Alto Adige", "Trentino-Alto Adige"},
		{Of, "Lombardia", "Lombardia"},
		{OfRegistryArea, "Trentino-Alto Adige/Südtirol", "Trentino-Alto Adige"},
		{OfRegistryArea, "Valle d'Aosta", "Piemonte"},
		{OfRegistryArea, "Basilicata", "Puglia"},
		{OfRegistryArea, "NaN", ""},
		{OfRegistryArea, "", ""},
		{OfRegistryArea, "Lazio", "Lazio"},
		{OfPositionState, "Valle d'Aosta", "Piemonte"},
		{OfPositionState, "Basilicata", "Puglia"},
		{OfPositionState, "Emilia-Romagna", "Emilia-Romagna"},
	}
	for _, c := range cases {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}
