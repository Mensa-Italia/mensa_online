package zincsearch

import (
	"encoding/json"
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"io"
	"log"
	"mensadb/tools/env"
	"net/http"
	"strings"
)

func UploadAllFiles(app core.App) {
	records, err := app.FindAllRecords("documents")
	if err != nil {
		return
	}
	for _, record := range records {
		id := record.Id
		title := record.GetString("name")
		recordDeep, err2 := app.FindRecordById("documents_elaborated", record.GetString("elaborated"))
		if err2 != nil {
			UploadFileToZinc(id, title, "")
		} else {
			UploadFileToZinc(id, title, recordDeep.GetString("ia_resume"))
		}
	}
}

func UploadFileToZinc(id, title, content string) {
	type ZincData struct {
		Id      string `json:"id"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	dataStruct := ZincData{
		Id:      id,
		Title:   title,
		Content: content,
	}
	dataBytes, err := json.Marshal(dataStruct)
	if err != nil {
		return
	}
	req, err := http.NewRequest("PUT", "https://search.svc.mensa.it/api/documents/_doc/"+id, strings.NewReader(string(dataBytes)))
	if err != nil {
		return
	}
	req.SetBasicAuth(env.GetZincUsername(), env.GetZincPassword())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.138 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	log.Println(resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	fmt.Println(string(body))
}
