package aipower

import (
	"context"
	"io"
	"log"

	"github.com/gabriel-vasile/mimetype"
	"github.com/pocketbase/pocketbase/tools/filesystem"
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
