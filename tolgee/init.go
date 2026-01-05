package tolgee

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
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

func Load(apikey string) {
	log.Println("Loading Tolgee translations")
	ak = apikey
	_ = GetLanguages()
	log.Println("Tolgee translations loaded total languages: ", len(translations))
}

//internal api
func GetLanguages() error {
	languagesData, err := resty.New().R().SetPathParams(
		map[string]string{
			"ak": ak,
		},
	).Get("https://i18n.svc.mensa.it/api/{ak}")
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
		translationData, err := resty.New().R().
			SetPathParams(
				map[string]string{
					"ak":       ak,
					"language": buildLang.Tag,
				},
			).
			SetQueryParam("nested", "true").
			Get("https://i18n.svc.mensa.it/api/{ak}/{language}")
		if err == nil {
			_ = json.Unmarshal(translationData.Body(), &buildLang.Tranlsations)
		}
		if buildLang.Base {
			baseLanguage = buildLang.Tag
		}
		languageList = append(languageList, buildLang)
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
	if len(strings.TrimSpace(lang)) > 1 {
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
		_ = GetLanguages()
		return getTranslationInternal(key, lang, namedArgs...)
	}
}

func getTranslationInternal(key string, lang string, namedArgs ...map[string]string) string {
	existinglangauge := baseLanguage

	// check if the language exists
	if len(strings.TrimSpace(lang)) > 1 {
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
