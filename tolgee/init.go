package tolgee

import (
	"encoding/json"
	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
	"strings"
)

type Language struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	Tag          string `json:"tag"`
	OriginalName string `json:"originalName"`
	FlagEmoji    string `json:"flagEmoji"`
	Base         bool   `json:"base"`
	Tranlsations map[string]string
}

var ak = ""
var translations map[string]Language
var baseLanguage = "en"

func main() {
	Load("tgpak_geydomjsl42wsmrwom2wooljgbyhezdrmnyg2zzzge4wenbrgbsa")
}

func Load(apikey string) {
	ak = apikey
	_ = GetLanguages()
}

func GetLanguages() error {
	languagesData, err := resty.New().R().SetQueryParams(
		map[string]string{
			"ak":   ak,
			"size": "1000",
		},
	).Get("https://app.tolgee.io/v2/projects/languages")
	if err != nil {
		return err
	}
	arrayOfLanguages := gjson.ParseBytes(languagesData.Body()).Get("_embedded.languages").Array()
	languageList := make([]Language, len(arrayOfLanguages))
	for _, language := range arrayOfLanguages {
		buildLang := Language{
			Id:           int(language.Get("id").Int()),
			Name:         language.Get("name").String(),
			Tag:          language.Get("tag").String(),
			OriginalName: language.Get("originalName").String(),
			FlagEmoji:    language.Get("flagEmoji").String(),
			Base:         language.Get("base").Bool(),
		}
		translationData, err := resty.New().R().SetQueryParams(
			map[string]string{
				"ak":                 ak,
				"size":               "1000",
				"languages":          buildLang.Tag,
				"format":             "JSON",
				"zip":                "false",
				"structureDelimiter": "",
			},
		).Get("https://app.tolgee.io/v2/projects/export")
		if err == nil {
			_ = json.Unmarshal(translationData.Body(), &buildLang.Tranlsations)
		}
		languageList = append(languageList, buildLang)
		if buildLang.Base {
			baseLanguage = buildLang.Tag
		}
	}
	translations = make(map[string]Language)
	for _, lang := range languageList {
		translations[lang.Tag] = lang
	}
	return nil
}

func GetTranslation(key string, lang string, namedArgs ...map[string]string) string {
	existinglangauge := baseLanguage

	// check if the language exists
	if _, ok := translations[lang]; ok {
		existinglangauge = lang
	} else {
		if len(strings.Split(lang, "_")) > 1 {
			lang = strings.Split(lang, "_")[0]
			if _, ok := translations[lang]; ok {
				existinglangauge = lang
			}
		}
	}

	if _, ok := translations[existinglangauge].Tranlsations[key]; ok {
		translationToUse := translations[existinglangauge].Tranlsations[key]
		if len(namedArgs) > 0 {
			for k, v := range namedArgs[0] {
				translationToUse = strings.ReplaceAll(translationToUse, "{"+k+"}", v)
			}
		}
		return translationToUse
	} else {
		return key
	}
}
