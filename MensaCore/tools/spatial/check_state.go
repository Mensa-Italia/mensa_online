package spatial

import (
	_ "embed"
	"sync"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/planar"
)

//go:embed Reg01012024_g_WGS84.geojson
var regioniGeoJSON []byte

var (
	regioniOnce sync.Once
	regioniFC   *geojson.FeatureCollection
	regioniErr  error
)

func loadRegioni() {
	regioniOnce.Do(func() {
		regioniFC, regioniErr = geojson.UnmarshalFeatureCollection(regioniGeoJSON)
	})
}

func LoadState(lat, lon float64) string {
	loadRegioni()
	if regioniErr != nil || regioniFC == nil {
		return "NaN"
	}

	orbPoint := orb.Point{lon, lat}
	for _, feature := range regioniFC.Features {
		if feature.Geometry == nil {
			continue
		}
		switch feature.Geometry.GeoJSONType() {
		case geojson.TypePolygon:
			polygon := feature.Geometry.(orb.Polygon)
			if planar.PolygonContains(polygon, orbPoint) {
				return IntToName(feature.Properties.MustInt("COD_REG"))
			}
		case geojson.TypeMultiPolygon:
			multiPolygon := feature.Geometry.(orb.MultiPolygon)
			if planar.MultiPolygonContains(multiPolygon, orbPoint) {
				return IntToName(feature.Properties.MustInt("COD_REG"))
			}
		}
	}
	return "NaN"
}

func IntToName(val int) string {
	switch val {
	case 1:
		return "Piemonte"
	case 2:
		return "Valle d'Aosta"
	case 3:
		return "Lombardia"
	case 4:
		return "Trentino-Alto Adige"
	case 5:
		return "Veneto"
	case 6:
		return "Friuli-Venezia Giulia"
	case 7:
		return "Liguria"
	case 8:
		return "Emilia-Romagna"
	case 9:
		return "Toscana"
	case 10:
		return "Umbria"
	case 11:
		return "Marche"
	case 12:
		return "Lazio"
	case 13:
		return "Abruzzo"
	case 14:
		return "Molise"
	case 15:
		return "Campania"
	case 16:
		return "Puglia"
	case 17:
		return "Basilicata"
	case 18:
		return "Calabria"
	case 19:
		return "Sicilia"
	case 20:
		return "Sardegna"
	}
	return "NaN"
}
