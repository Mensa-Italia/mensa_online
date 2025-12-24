package aitools

import (
	"bytes"
	"context"
	"io"
	"log"

	"github.com/gabriel-vasile/mimetype"
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
