package spatial

import (
	_ "embed"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
)

//go:embed province.json
var provinceJSON []byte

var (
	provinceOnce  sync.Once
	provinceIndex map[string]string // lowercase nome -> regione
	provinceList  []provinceEntry   // per il fallback substring
)

type provinceEntry struct {
	name   string // lowercase
	region string
}

func loadProvinces() {
	provinceOnce.Do(func() {
		provinceIndex = make(map[string]string, 128)
		for _, item := range gjson.ParseBytes(provinceJSON).Array() {
			name := strings.ToLower(item.Get("nome").String())
			region := item.Get("regione").String()
			if name == "" || region == "" {
				continue
			}
			provinceIndex[name] = region
			provinceList = append(provinceList, provinceEntry{name: name, region: region})
		}
	})
}

func CheckProvinceFromState(state string) string {
	loadProvinces()
	if state == "" {
		return "NaN"
	}
	key := strings.ToLower(strings.TrimSpace(state))
	if region, ok := provinceIndex[key]; ok {
		return region
	}
	for _, p := range provinceList {
		if strings.Contains(p.name, key) {
			return p.region
		}
	}
	return "NaN"
}
