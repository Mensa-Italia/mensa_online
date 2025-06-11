package aipower

import (
	"context"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
	"google.golang.org/genai"
	"log"
	"mensadb/tools/env"
)

func uploadToGemini(fileSystemData *filesystem.File) *genai.File {
	ctx := context.Background()
	client, _ := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})
	options := genai.UploadFileConfig{
		DisplayName: fileSystemData.Name,
	}
	open, err := fileSystemData.Reader.Open()
	if err != nil {
		return nil
	}
	fileData, err := client.Files.Upload(ctx, open, &options)
	if err != nil {
		log.Fatalf("Error uploading file: %v", err)
		return nil
	}
	log.Printf("Uploaded file %s as: %s", fileData.DisplayName, fileData.URI)
	return fileData
}

func AskResume(fileSystemData *filesystem.File) string {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  env.GetGeminiKey(),
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return ""
	}

	temp := float32(1)
	topP := float32(0.95)
	topK := float32(40.0)
	maxOutputTokens := int32(8192)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		Temperature:      &temp,
		TopP:             &topP,
		TopK:             &topK,
		MaxOutputTokens:  maxOutputTokens,
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

	uploadedFile := uploadToGemini(fileSystemData)

	parts := []*genai.Part{
		&genai.Part{
			FileData: &genai.FileData{
				FileURI: uploadedFile.URI,
			},
		},
		genai.NewPartFromText(env.GetGeminiResumePrompt()),
	}

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		[]*genai.Content{
			genai.NewContentFromParts(parts, genai.RoleUser),
		},
		config,
	)
	if err != nil {
		return ""
	}
	data := gjson.Parse(result.Text())
	if data.Get("resume_text").Exists() {
		return data.Get("resume_text").String()
	}

	return ""
}
