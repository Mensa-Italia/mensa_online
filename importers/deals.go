package importers

import (
	"encoding/csv"
	"encoding/json"
	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
	"log"
	"os"
	"strings"
	"time"
)

// ID,NOME AZIENDA,C.F./P.IVA,SETTORE,COPERTURA,REGIONE,INDIRIZZO,OFFERTA,BENEFICIARI,DATA INIZIO,SCADENZA,MODALITA' DI ACCESSO,MODULI,Contatti (Da non divulgare)
type Deal struct {
	ID                string `json:"ID"`
	NomeAzienda       string `json:"NOME AZIENDA"`
	CFPIVA            string `json:"C.F./P.IVA"`
	Settore           string `json:"SETTORE"`
	Copertura         string `json:"COPERTURA"`
	Regione           string `json:"REGIONE"`
	Indirizzo         string `json:"INDIRIZZO"`
	Offerta           string `json:"OFFERTA"`
	Beneficiari       string `json:"BENEFICIARI"`
	DataInizio        string `json:"DATA INIZIO"`
	Scadenza          string `json:"SCADENZA"`
	ModalitaDiAccesso string `json:"MODALITA DI ACCESSO"`
	Moduli            string `json:"MODULI"`
	Contatti          string `json:"Contatti (Da non divulgare)"`
}

func readCsvFile(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		log.Fatal("Unable to read input file "+filePath, err)
	}
	defer f.Close()

	csvReader := csv.NewReader(f)
	records, err := csvReader.ReadAll()
	if err != nil {
		log.Fatal("Unable to parse file as CSV for "+filePath, err)
	}

	var jsonRecords []map[string]string
	columnNames := records[0]
	for i := 1; i < len(records); i++ {
		jsonRecord := make(map[string]string)
		for j := 0; j < len(columnNames); j++ {
			if columnNames[j] == "MODALITA' DI ACCESSO" {
				columnNames[j] = "MODALITA DI ACCESSO"
			}
			jsonRecord[columnNames[j]] = records[i][j]
		}
		jsonRecords = append(jsonRecords, jsonRecord)
	}

	marshal, err := json.Marshal(jsonRecords)
	if err != nil {
		return "[]"
	}

	return string(marshal)
}

func ReadDeals() []Deal {
	filePath := "importers/deals.csv"
	jsonData := readCsvFile(filePath)

	var deals []Deal
	err := json.Unmarshal([]byte(jsonData), &deals)
	if err != nil {
		log.Fatal("Unable to parse JSON data", err)
	}

	for _, deal := range deals {
		beneficiari := ""

		if strings.Contains(deal.Beneficiari, "Soci") {
			beneficiari = "active_members"
		}
		if strings.Contains(deal.Beneficiari, "famigliari") {
			beneficiari = "active_members and relatives"
		}

		// parse day/month/year
		startingDate, _ := time.Parse("02/01/2006", deal.DataInizio)
		endingDate, _ := time.Parse("02/01/2006", deal.Scadenza)

		res, _ := resty.New().R().SetBody(map[string]any{
			"name":              deal.NomeAzienda,
			"commercial_sector": deal.Settore,
			"is_local":          strings.ToLower(strings.TrimSpace(deal.Copertura)) == "locale",
			"details":           deal.Offerta,
			"who":               beneficiari,
			"starting":          startingDate.UTC(),
			"ending":            endingDate.UTC(),
			"how_to_get":        deal.ModalitaDiAccesso,
			"link":              deal.Moduli,
			"position":          getLatLong(deal.Indirizzo),
			"owner":             "5723",
			"is_active":         true,
			"vat_number":        deal.CFPIVA,
		}).Post("https://svc.mensa.it/api/collections/deals/records")

		dealId := gjson.ParseBytes(res.Body()).Get("id").String()
		if dealId != "" {
			getPrivateInfos(deal.Contatti, dealId)
		}
	}

	return deals
}

func getLatLong(string2 string) *string {
	get, err := resty.New().R().
		SetQueryParam("q", string2).
		Get("https://photon.komoot.io/api/")
	if err != nil {
		return nil
	}

	res := gjson.ParseBytes(get.Body()).Get("features.0.geometry.coordinates").Array()
	if len(res) == 0 {
		return nil
	}

	lat := res[1].Float()
	long := res[0].Float()

	post, err := resty.New().R().SetBody(map[string]any{
		"lat":  lat,
		"lon":  long,
		"name": string2,
	}).Post("https://svc.mensa.it/api/collections/positions/records")
	if err != nil {
		return nil
	}

	res2 := gjson.ParseBytes(post.Body()).Get("id").String()
	if res2 == "" {
		return nil
	}

	return &res2
}

func getPrivateInfos(string2 string, dealId string) string {
	res, _ := resty.New().R().
		SetBody(map[string]any{
			"note": string2,
			"deal": dealId,
		}).
		Post("https://svc.mensa.it/api/collections/deals_contacts/records")
	return gjson.ParseBytes(res.Body()).Get("id").String()
}
