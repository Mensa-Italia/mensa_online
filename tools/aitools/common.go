package aitools

import (
	"bytes"
	"context"
	"io"
	"log"

	"github.com/gabriel-vasile/mimetype"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"google.golang.org/genai"
)

func prepareFile(client *genai.Client, name string, data []byte) *genai.File {
	ctx := context.Background()
	log.Println("Uploading file to Gemini:", name)

	openedFile := io.NopCloser(bytes.NewReader(data))
	defer func() { _ = openedFile.Close() }()

	mtype, err := mimetype.DetectReader(openedFile)
	if err != nil {
		log.Printf("Error detecting MIME type for %s: %v", name, err)
		return nil
	}
	detectedMIME := mtype.String()

	uploadData := data
	uploadMIME := detectedMIME

	if !geminiSupportedMIMETypes[baseMIME(detectedMIME)] {
		log.Printf("MIME type %q not supported by Gemini for %s — attempting text extraction", detectedMIME, name)
		converted, convertedMIME, convErr := convertToTextFallback(data, detectedMIME, name)
		if convErr != nil {
			log.Printf("Text extraction failed for %s (%s): %v — skipping", name, detectedMIME, convErr)
			return nil
		}
		uploadData = converted
		uploadMIME = convertedMIME
		log.Printf("Text extraction succeeded for %s (%d bytes as %s)", name, len(uploadData), uploadMIME)
	}

	options := genai.UploadFileConfig{
		DisplayName: name,
		MIMEType:    uploadMIME,
	}

	fileData, err := client.Files.Upload(ctx, io.NopCloser(bytes.NewReader(uploadData)), &options)
	if err != nil {
		log.Printf("Error uploading file %s: %v", name, err)
		return nil
	}
	return fileData
}

func UploadFileToAIClient(client *genai.Client, reader *filesystem.File) *genai.File {
	open, err := reader.Reader.Open()
	defer func() { _ = open.Close() }()
	if err != nil {
		log.Println("Error generating content:", err)
		return nil
	}
	data, err := io.ReadAll(open)
	if err != nil {
		log.Println("Error generating content:", err)
		return nil
	}
	usageFile := prepareFile(client, reader.Name, data)
	if usageFile == nil {
		log.Println("Error generating content:", err)
		return nil
	}

	return usageFile
}
