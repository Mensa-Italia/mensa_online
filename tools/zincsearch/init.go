package zincsearch

import (
	"fmt"
	"github.com/pocketbase/pocketbase"
	"io"
	"log"
	"mensadb/tools/env"
	"net/http"
	"strings"
)

func UploadAllFiles(app *pocketbase.PocketBase) {
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
	data := fmt.Sprintf(`{
	"id": "%s",
	"title": "%s",
	"content": "%s"
}`, id, title, content)
	req, err := http.NewRequest("POST", "https://search.svc.mensa.it/api/documents/_doc/"+id, strings.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	req.SetBasicAuth(env.GetZincUsername(), env.GetZincPassword())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.138 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	log.Println(resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(body))
}
