package aitools

import (
	"context"
	"io"
	"log"
	"mensadb/tools/env"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
	"google.golang.org/genai"
)

func ResumeDocument(app core.App, reader *filesystem.File) string {
	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})

	open, err := reader.Reader.Open()
	if err != nil {
		log.Println("Error generating content:", err)
		return ""
	}
	data, err := io.ReadAll(open)
	if err != nil {
		log.Println("Error generating content:", err)
		return ""
	}
	usageFile := prepareFile(client, reader.Name, data)
	if usageFile == nil {
		log.Println("Error generating content:", err)
		return ""
	}

	treeOfDocumentsIds, err := app.FindRecordsByIds("documents", FindTree(app, reader).RetrieveIDs())
	if err != nil {
		log.Println("Error generating content:", err)
		return ""
	}

	fsys, _ := app.NewFilesystem()
	defer fsys.Close()

	var listOfCitatedFiles []*genai.Part
	for _, record := range treeOfDocumentsIds {
		key := record.BaseFilesPath() + "/" + record.GetString("file")
		file, err := fsys.GetReuploadableFile(key, true)
		if err != nil {
			log.Println("Error generating content:", err)
			continue
		}
		open, err := file.Reader.Open()
		if err != nil {
			log.Println("Error generating content:", err)
			continue
		}
		data, err := io.ReadAll(open)
		if err != nil {
			log.Println("Error generating content:", err)
			continue
		}

		g := prepareFile(client, file.Name, data)
		if g == nil {
			continue
		}
		listOfCitatedFiles = append(listOfCitatedFiles, genai.NewPartFromFile(*g))
	}

	listOfCitatedFiles = append(listOfCitatedFiles,
		genai.NewPartFromFile(*usageFile),
		genai.NewPartFromText(strings.ReplaceAll(provaPrompt, "{nameFile}", reader.Name)),
	)
	contents := []*genai.Content{
		{
			Role:  genai.RoleUser,
			Parts: listOfCitatedFiles,
		},
	}

	config := &genai.GenerateContentConfig{
		ThinkingConfig: &genai.ThinkingConfig{
			ThinkingLevel: genai.ThinkingLevelHigh,
		},
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type:     genai.TypeObject,
			Required: []string{"resume_text"},
			Properties: map[string]*genai.Schema{
				"resume_text": &genai.Schema{
					Type: genai.TypeString,
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
		log.Println("Error generating content:", err)
		return ""
	}

	log.Println(result.Text())

	dataResult := gjson.Parse(result.Text())
	if dataResult.Get("resume_text").Exists() {
		return dataResult.Get("resume_text").String()
	}

	return ""

}
