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

	options := genai.UploadFileConfig{
		DisplayName: name,
		MIMEType:    mtype.String(),
	}

	// Reset reader to beginning after MIME detection
	if seeker, ok := openedFile.(io.Seeker); ok {
		_, _ = seeker.Seek(0, io.SeekStart)
	} else {
		// NopCloser over bytes.Reader is a Seeker, but keep a safe fallback.
		openedFile = io.NopCloser(bytes.NewReader(data))
	}

	fileData, err := client.Files.Upload(ctx, openedFile, &options)
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
