package aitools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"google.golang.org/genai"
)

// maxUploadBytes is the safe upper bound for text sent to Gemini in a single call.
// Gemini's hard limit is 1 048 576 tokens; at ~4 chars/token this is ~4 MB,
// but we stay well below to leave headroom for the prompt itself.
const maxUploadBytes = 800_000

// chunkSize is the target size for each chunk when splitting large text.
const chunkSize = 500_000

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

	if len(uploadData) > maxUploadBytes {
		log.Printf("Extracted text too large (%d bytes) for %s — reducing via per-section summarization", len(uploadData), name)
		reduced, reduceErr := reduceToSummary(client, data, detectedMIME, uploadData, name)
		if reduceErr != nil {
			log.Printf("Section summarization failed for %s: %v — skipping", name, reduceErr)
			return nil
		}
		uploadData = reduced
		uploadMIME = "text/plain"
		log.Printf("Reduced to %d bytes for %s", len(uploadData), name)
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

// reduceToSummary shrinks extracted text that exceeds maxUploadBytes.
// For archives (ZIP/RAR) each entry is summarized individually.
// For other formats the text is split into chunks and each chunk is summarized.
func reduceToSummary(client *genai.Client, originalData []byte, mimeType string, extractedText []byte, name string) ([]byte, error) {
	base := baseMIME(mimeType)

	switch base {
	case "application/zip", "application/x-zip-compressed", "application/x-zip":
		entries, err := extractZipEntries(originalData)
		if err != nil {
			return nil, err
		}
		result := summarizeArchiveEntries(client, entries, name)
		return []byte(result), nil

	case "application/vnd.rar", "application/x-rar-compressed", "application/x-rar":
		entries, err := extractRarEntries(originalData)
		if err != nil {
			return nil, err
		}
		result := summarizeArchiveEntries(client, entries, name)
		return []byte(result), nil

	default:
		result := summarizeInChunks(client, string(extractedText), name, 1)
		return []byte(result), nil
	}
}

// maxSummarizeDepth evita loop infiniti nel caso i riassunti siano ancora troppo grandi.
const maxSummarizeDepth = 4

// summarizeArchiveEntries riassume ogni voce di un archivio con Gemini
// e restituisce i riassunti concatenati. Se il risultato è ancora troppo
// grande viene ridotto ulteriormente con summarizeInChunks.
func summarizeArchiveEntries(client *genai.Client, entries []archiveEntry, archiveName string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Contenuto dell'archivio %s (riassunto per file):\n\n", archiveName)
	for _, entry := range entries {
		summary := summarizeSection(client, entry.Text, entry.Name)
		if summary != "" {
			fmt.Fprintf(&sb, "### %s\n%s\n\n", entry.Name, summary)
		}
	}
	result := sb.String()
	if len(result) > maxUploadBytes {
		log.Printf("summarizeArchiveEntries: combined summaries still too large (%d bytes) for %s — reducing further", len(result), archiveName)
		result = summarizeInChunks(client, result, archiveName, 1)
	}
	return result
}

// summarizeInChunks divide il testo in blocchi da chunkSize byte,
// riassume ogni blocco con Gemini e concatena i risultati.
// Se il risultato combinato è ancora troppo grande si chiama ricorsivamente
// fino a maxSummarizeDepth livelli.
func summarizeInChunks(client *genai.Client, text, name string, depth int) string {
	if len(text) <= maxUploadBytes {
		return text
	}
	if depth > maxSummarizeDepth {
		log.Printf("summarizeInChunks: max depth reached for %s — hard truncating", name)
		return text[:maxUploadBytes]
	}
	total := (len(text) + chunkSize - 1) / chunkSize
	var parts []string
	for i := 0; i < len(text); i += chunkSize {
		end := i + chunkSize
		if end > len(text) {
			end = len(text)
		}
		label := fmt.Sprintf("%s (parte %d/%d)", name, i/chunkSize+1, total)
		summary := summarizeSection(client, text[i:end], label)
		if summary != "" {
			parts = append(parts, summary)
		}
	}
	combined := strings.Join(parts, "\n\n---\n\n")
	if len(combined) > maxUploadBytes {
		log.Printf("summarizeInChunks: combined still too large (%d bytes) at depth %d for %s — going deeper", len(combined), depth, name)
		return summarizeInChunks(client, combined, name, depth+1)
	}
	return combined
}

// summarizeSection invia un blocco di testo a Gemini e restituisce il riassunto.
// Se il testo è ancora troppo grande per il modello viene troncato come ultima risorsa.
func summarizeSection(client *genai.Client, text, label string) string {
	ctx := context.Background()

	if len(text) > maxUploadBytes {
		text = text[:maxUploadBytes]
	}

	prompt := fmt.Sprintf(
		"Riassumi il contenuto del seguente testo estratto da '%s', "+
			"mantenendo tutte le informazioni rilevanti in modo conciso:\n\n%s",
		label, text,
	)

	result, err := client.Models.GenerateContent(
		ctx,
		"gemini-2.0-flash",
		[]*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{genai.NewPartFromText(prompt)}}},
		nil,
	)
	if err != nil {
		log.Printf("summarizeSection: Gemini error for %q: %v", label, err)
		if len(text) > 2000 {
			return text[:2000] + "…[troncato]"
		}
		return text
	}
	return result.Text()
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
