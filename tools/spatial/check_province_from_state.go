package spatial

import (
	"github.com/tidwall/gjson"
	"os"
	"strings"
)

func CheckProvinceFromState(state string) string {
	dataRead, err := os.ReadFile("pb_public/province.json")
	if err != nil {
		return "NaN"
	}
	province := gjson.ParseBytes(dataRead)

	for _, item := range province.Array() {
		if strings.ToLower(item.Get("nome").String()) == strings.ToLower(state) {
			return item.Get("regione").String()
		}
	}
	for _, item := range province.Array() {
		if strings.Contains(strings.ToLower(item.Get("nome").String()), strings.ToLower(state)) {
			return item.Get("regione").String()
		}
	}
	return "NaN"
}
