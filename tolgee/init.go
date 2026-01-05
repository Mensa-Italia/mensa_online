package tolgee

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/pocketbase/pocketbase/core"
)

type Language struct {
	Tag          string `json:"tag"`
	Tranlsations map[string]string
}

var ak = ""
var translations map[string]Language
var baseLanguage = "en"

func Load(apikey string, app core.App) {
	log.Println("Loading Tolgee translations")
	ak = apikey
	_ = GetLanguages(app)
	log.Println("Tolgee translations loaded total languages: ", len(translations))
}

func GetLanguages(app core.App) error {
	arrayOfLanguages := strings.Split(getInternalConfig(app, "languages"), ",")

	languageList := make([]Language, len(arrayOfLanguages))
	for _, language := range arrayOfLanguages {
		buildLang := Language{
			Tag: language,
		}
		translationData, err := resty.New().R().
			Get(strings.ReplaceAll(getInternalConfig(app, "i18n_flat_url"), "{locale}", buildLang.Tag))
		if err == nil {
			_ = json.Unmarshal(translationData.Body(), &buildLang.Tranlsations)
		}
		if language == getInternalConfig(app, "base_language") {
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
		//_ = GetLanguages()
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

func getInternalConfig(app core.App, key string) string {
	collection, err := app.FindCollectionByNameOrId("configs")
	if err != nil {
		return ""
	}

	record, err := app.FindFirstRecordByData(collection.Id, "key", key)
	if err != nil || record == nil {
		return ""
	}

	return record.GetString("value")
}
