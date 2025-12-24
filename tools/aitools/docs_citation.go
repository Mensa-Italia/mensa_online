package aitools

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"mensadb/tools/env"
	"strings"

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

const provaPrompt = ``

func FindTree(file *filesystem.File) DocumentsCitationList {
	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})

	open, err := file.Reader.Open()
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(open)
	if err != nil {
		return nil
	}

	uploaded := prepareFile(client, file.Name, data)
	if uploaded == nil {
		log.Fatal("upload to Gemini failed")
	}

	contents := []*genai.Content{
		{
			Role: genai.RoleUser,
			Parts: []*genai.Part{
				genai.NewPartFromFile(*uploaded),
				genai.NewPartFromText(strings.ReplaceAll(provaPrompt, "{nameFile}", file.Name)),
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
