package aitools

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"google.golang.org/genai"
)

type DocumentCitation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DocumentsCitationList []DocumentCitation

func (dcl DocumentsCitationList) RetrieveIDs() []string {
	ids := []string{}
	for _, doc := range dcl {
		ids = append(ids, doc.ID)
	}
	return ids
}

type documentsCitationResponse struct {
	Items []DocumentCitation `json:"documents_list"`
}

const provaPrompt = `{docs}
---------
individua quali documenti sono citati nel documento {nameFile}`

func FindTree(app core.App, file *filesystem.File) DocumentsCitationList {
	ctx := context.Background()
	client := GetAIClient()

	uploaded := UploadFileToAIClient(client, file)

	promptTemp := strings.ReplaceAll(provaPrompt, "{nameFile}", file.Name)
	promptTemp = strings.ReplaceAll(promptTemp, "{docs}", retrieveAllDocumentsList(app))

	contents := []*genai.Content{
		{
			Role: genai.RoleUser,
			Parts: []*genai.Part{
				genai.NewPartFromFile(*uploaded),
				genai.NewPartFromText(promptTemp),
			},
		},
	}

	config := &genai.GenerateContentConfig{
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingLevel: genai.ThinkingLevelHigh,
		},
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type:     genai.TypeObject,
			Required: []string{"documents_list"},
			Properties: map[string]*genai.Schema{
				"documents_list": &genai.Schema{
					Type: genai.TypeArray,
					Items: &genai.Schema{
						Type:     genai.TypeObject,
						Required: []string{"id", "name"},
						Properties: map[string]*genai.Schema{
							"id": &genai.Schema{
								Type: genai.TypeString,
							},
							"name": &genai.Schema{
								Type: genai.TypeString,
							},
						},
					},
				},
			},
		},
	}

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-3-flash-preview",
		contents,
		config,
	)

	if err != nil {
		log.Fatal(err)
	}

	var responseCitation documentsCitationResponse
	_ = json.Unmarshal([]byte(result.Text()), &responseCitation)

	log.Println(responseCitation)

	return responseCitation.Items
}

func retrieveAllDocumentsList(app core.App) string {
	records, _ := app.FindAllRecords("documents")
	type Doc struct {
		ID   string
		Name string
	}

	var docs []Doc
	for _, record := range records {
		docs = append(docs, Doc{
			ID:   record.Id,
			Name: record.GetString("name"),
		})
	}
	data, _ := json.Marshal(docs)
	return string(data)
}
