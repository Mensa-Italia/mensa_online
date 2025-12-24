package aipower

import (
	"context"
	"io"
	"log"
	"mensadb/tools/env"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/tidwall/gjson"
	"google.golang.org/genai"
)

func uploadToGemini(client *genai.Client, fileSystemData *filesystem.File) *genai.File {

	ctx := context.Background()
	openedFile, err := fileSystemData.Reader.Open()
	if err != nil {
		return nil
	}
	log.Println("Uploading file to Gemini:", fileSystemData.Name)
	mtype, err := mimetype.DetectReader(openedFile)
	options := genai.UploadFileConfig{
		DisplayName: fileSystemData.Name,
		MIMEType:    mtype.String(),
	}
	// Reset reader to beginning after MIME detection
	if seeker, ok := openedFile.(io.Seeker); ok {
		_, _ = seeker.Seek(0, io.SeekStart)
	} else {
		_ = openedFile.Close()
		openedFile, err = fileSystemData.Reader.Open()
		if err != nil {
			return nil
		}
	}
	fileData, err := client.Files.Upload(ctx, openedFile, &options)
	if err != nil {
		log.Fatalf("Error uploading file: %v", err)
		return nil
	}
	return fileData
}

func AskResume(fileSystemData *filesystem.File, appendedFiles []*filesystem.File) string {
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

	uploadedFileCheck := uploadToGemini(client, fileSystemData)
	uploadedFileAppended := []*genai.File{}
	for _, f := range appendedFiles {
		uploaded := uploadToGemini(client, f)
		if uploaded != nil {
			uploadedFileAppended = append(uploadedFileAppended, uploaded)
		}
	}

	log.Println(uploadedFileCheck.MIMEType)
	parts := []*genai.Part{
		&genai.Part{
			FileData: &genai.FileData{
				FileURI: uploadedFileCheck.URI,
			},
		},
	}

	for _, f := range uploadedFileAppended {
		parts = append(parts, &genai.Part{
			FileData: &genai.FileData{
				FileURI: f.URI,
			},
		})
	}

	parts = append(parts, genai.NewPartFromText(strings.ReplaceAll(env.GetGeminiResumePrompt(), "{file_name}", fileSystemData.Name)))

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
