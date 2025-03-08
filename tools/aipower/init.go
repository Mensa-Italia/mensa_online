package aipower

import (
	"context"
	"encoding/json"
	"github.com/google/generative-ai-go/genai"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"google.golang.org/api/option"
	"log"
	"mensadb/tools/env"
)

func uploadToGemini(ctx context.Context, client *genai.Client, fileSystemData *filesystem.File) string {
	options := genai.UploadFileOptions{
		DisplayName: fileSystemData.Name,
	}
	open, err := fileSystemData.Reader.Open()
	if err != nil {
		return ""
	}
	fileData, err := client.UploadFile(ctx, "", open, &options)
	if err != nil {
		log.Fatalf("Error uploading file: %v", err)
		return ""
	}
	log.Printf("Uploaded file %s as: %s", fileData.DisplayName, fileData.URI)
	return fileData.URI
}

func AskResume(fileSystemData *filesystem.File) string {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(env.GetGeminiKey()))
	if err != nil {
		log.Fatalf("Error creating client: %v", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-2.0-flash")

	model.SetTemperature(1)
	model.SetTopK(40)
	model.SetTopP(0.95)
	model.SetMaxOutputTokens(8192)
	model.ResponseMIMEType = "application/json"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text("PARLI SOLO ITALIANO")},
	}
	model.ResponseSchema = &genai.Schema{
		Type:     genai.TypeObject,
		Required: []string{"resume_text"},
		Properties: map[string]*genai.Schema{
			"resume_text": &genai.Schema{
				Type: genai.TypeString,
			},
		},
	}

	fileURIs := []string{
		uploadToGemini(ctx, client, fileSystemData),
	}

	session := model.StartChat()
	session.History = []*genai.Content{
		{
			Role: "user",
			Parts: []genai.Part{
				genai.FileData{URI: fileURIs[0]},
				genai.Text("Dobbiamo creare il riassunto e il titolo per la pagina di un'app che precede il documento caricato dal consiglio. Mi serve che mi crei un riassunto del documento che abbia al suo interno tutti gli elementi per comprendere il documento senza leggerlo in italiano."),
			},
		},
	}

	resp, err := session.SendMessage(ctx, genai.Text("Dobbiamo creare il riassunto e il titolo per la pagina di un'app che precede il documento caricato dal consiglio. Mi serve che mi crei un riassunto del documento che abbia al suo interno tutti gli elementi per comprendere il documento senza leggerlo in italiano."))
	if err != nil {
		return ""
	}

	if len(resp.Candidates) > 0 {
		for _, part := range resp.Candidates[0].Content.Parts {
			if textPart, ok := part.(genai.Text); ok {
				var jsonData map[string]string
				err = json.Unmarshal([]byte(textPart), &jsonData)
				if err == nil {
					return jsonData["resume_text"]
				}
			}
		}
	}

	return ""
}
