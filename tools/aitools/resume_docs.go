package aitools

import (
	"context"
	"log"
	"mensadb/tools/env"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
	"google.golang.org/genai"
)

// resumeDocumentTimeout copre upload di più file + GenerateContent con thinking high.
// Se Gemini non risponde entro questo limite consideriamo l'operazione fallita
// e ritorniamo un placeholder vuoto: il documento sarà comunque visibile,
// il riassunto verrà rigenerato al prossimo retry.
const resumeDocumentTimeout = 120 * time.Second

func ResumeDocument(app core.App, reader *filesystem.File) string {
	ctx, cancel := context.WithTimeout(context.Background(), resumeDocumentTimeout)
	defer cancel()
	client := GetAIClient()
	if client == nil {
		log.Printf("ResumeDocument: gemini client unavailable for %s — fallback to empty resume", reader.Name)
		return ""
	}

	usageFile := UploadFileToAIClient(client, reader)
	if usageFile == nil {
		log.Printf("ResumeDocument: upload failed for %s, aborting", reader.Name)
		return ""
	}

	treeOfDocumentsIds, err := app.FindRecordsByIds("documents", FindTree(app, reader).RetrieveIDs())
	if err != nil {
		log.Println("Error generating content:", err)
		return ""
	}

	fsys, _ := app.NewFilesystem()
	defer func() { _ = fsys.Close() }()

	var listOfCitatedFiles []*genai.Part
	for _, record := range treeOfDocumentsIds {
		key := record.BaseFilesPath() + "/" + record.GetString("file")
		file, err := fsys.GetReuploadableFile(key, true)
		if err != nil {
			log.Println("Error generating content:", err)
			continue
		}
		g := UploadFileToAIClient(client, file)
		if g == nil {
			continue
		}
		listOfCitatedFiles = append(listOfCitatedFiles, genai.NewPartFromFile(*g))
	}

	listOfCitatedFiles = append(listOfCitatedFiles,
		genai.NewPartFromFile(*usageFile),
		genai.NewPartFromText(strings.ReplaceAll(env.GetGeminiResumePrompt(), "{nameFile}", reader.Name)),
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
		log.Printf("ResumeDocument: gemini summarize failed for %s: %v — fallback to empty resume", reader.Name, err)
		return ""
	}

	log.Println(result.Text())

	dataResult := gjson.Parse(result.Text())
	if dataResult.Get("resume_text").Exists() {
		return dataResult.Get("resume_text").String()
	}

	return ""

}
