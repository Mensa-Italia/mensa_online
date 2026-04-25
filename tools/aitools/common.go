package aitools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"os"
	"os/exec"

	"github.com/gabriel-vasile/mimetype"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"google.golang.org/genai"
)

// largePDFThreshold è la soglia oltre la quale si tenta la compressione del PDF prima
// di inviarlo a Gemini (10 MB).
const largePDFThreshold = 10_000_000

// compressPDF riduce le dimensioni di un PDF abbassando la qualità delle immagini
// embedded. Prova prima ghostscript (più efficace), poi pdfcpu come fallback.
// Restituisce i byte originali se entrambi falliscono o non portano vantaggi.
func compressPDF(data []byte) []byte {
	if result := compressPDFWithGhostscript(data); result != nil {
		return result
	}
	return compressPDFWithPdfcpu(data)
}

// compressPDFWithGhostscript usa gs per ridurre qualità immagini e stream.
// Ritorna nil se gs non è disponibile o la compressione fallisce.
func compressPDFWithGhostscript(data []byte) []byte {
	gsPath, err := exec.LookPath("gs")
	if err != nil {
		return nil
	}

	inFile, err := os.CreateTemp("", "pdf-in-*.pdf")
	if err != nil {
		return nil
	}
	defer func() { _ = os.Remove(inFile.Name()) }()

	outFile, err := os.CreateTemp("", "pdf-out-*.pdf")
	if err != nil {
		_ = inFile.Close()
		return nil
	}
	defer func() { _ = os.Remove(outFile.Name()) }()
	_ = outFile.Close()

	if _, err := inFile.Write(data); err != nil {
		_ = inFile.Close()
		return nil
	}
	_ = inFile.Close()

	cmd := exec.Command(gsPath,
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.4",
		"-dPDFSETTINGS=/ebook", // 150 dpi — leggibile da Gemini, molto più leggero
		"-dNOPAUSE",
		"-dQUIET",
		"-dBATCH",
		"-sOutputFile="+outFile.Name(),
		inFile.Name(),
	)
	if err := cmd.Run(); err != nil {
		log.Printf("compressPDF: ghostscript fallito (%v) — provo pdfcpu", err)
		return nil
	}

	result, err := os.ReadFile(outFile.Name())
	if err != nil || len(result) == 0 || len(result) >= len(data) {
		return nil
	}

	log.Printf("compressPDF (gs): %d → %d byte (-%d%%)", len(data), len(result),
		100*(len(data)-len(result))/len(data))
	return result
}

// compressPDFWithPdfcpu ottimizza un PDF con pdfcpu (pure Go, nessun binario esterno).
// Non riduce la qualità delle immagini ma comprime stream e rimuove oggetti ridondanti.
func compressPDFWithPdfcpu(data []byte) []byte {
	in := bytes.NewReader(data)
	var out bytes.Buffer
	if err := pdfcpuapi.Optimize(in, &out, nil); err != nil {
		log.Printf("compressPDF (pdfcpu): ottimizzazione fallita (%v) — uso originale", err)
		return data
	}
	compressed := out.Bytes()
	if len(compressed) >= len(data) {
		return data
	}
	log.Printf("compressPDF (pdfcpu): %d → %d byte (-%d%%)", len(data), len(compressed),
		100*(len(data)-len(compressed))/len(data))
	return compressed
}

// maxUploadBytes is the safe upper bound for text sent to Gemini in a single call.
// Gemini's hard limit is 1 048 576 tokens; at ~4 chars/token this is ~4 MB,
// but we stay well below to leave headroom for the prompt itself.
const maxUploadBytes = 800_000

// chunkSize is the target size for each chunk when splitting large text.
const chunkSize = 500_000

// uploadTimeout limita il tempo per il singolo upload a Gemini.
// Anche per file grossi (post-compressione) 90s è sufficiente sulla rete tipica.
const uploadTimeout = 90 * time.Second

// summarizeSectionTimeout limita ogni chiamata Generate per chunk/sezione.
const summarizeSectionTimeout = 60 * time.Second

func prepareFile(client *genai.Client, name string, data []byte) *genai.File {
	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
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

	// Comprimi i PDF grandi prima di inviarli a Gemini.
	if baseMIME(detectedMIME) == "application/pdf" && len(uploadData) > largePDFThreshold {
		log.Printf("PDF grande (%d byte) per %s — tentativo compressione", len(uploadData), name)
		uploadData = compressPDF(uploadData)
	}

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

		// Il check dimensione si applica solo al testo estratto da formati non supportati.
		// I formati nativi (PDF, immagini…) vengono caricati direttamente via Files API
		// che accetta fino a 2 GB; il limite di token riguarda il contenuto, non il file.
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
		var summary string
		if len(entry.Text) > maxUploadBytes {
			summary = summarizeInChunks(client, entry.Text, entry.Name, 1)
		} else {
			summary = summarizeSection(client, entry.Text, entry.Name)
		}
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
// In caso di 429 (rate limit) ritenta con backoff esponenziale fino a maxRetries volte.
// Se il testo è ancora troppo grande viene troncato come ultima risorsa.
func summarizeSection(client *genai.Client, text, label string) string {
	if len(text) > maxUploadBytes {
		text = text[:maxUploadBytes]
	}

	prompt := fmt.Sprintf(
		"Riassumi il contenuto del seguente testo estratto da '%s', "+
			"mantenendo tutte le informazioni rilevanti in modo conciso:\n\n%s",
		label, text,
	)

	const maxRetries = 5
	wait := 4 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), summarizeSectionTimeout)
		result, err := client.Models.GenerateContent(
			ctx,
			"gemini-2.0-flash",
			[]*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{genai.NewPartFromText(prompt)}}},
			nil,
		)
		cancel()
		if err == nil {
			return result.Text()
		}

		errStr := err.Error()
		isRateLimit := strings.Contains(errStr, "429") || strings.Contains(errStr, "RESOURCE_EXHAUSTED")
		if isRateLimit && attempt < maxRetries {
			log.Printf("summarizeSection: rate limit per %q (tentativo %d/%d) — attendo %s", label, attempt, maxRetries, wait)
			time.Sleep(wait)
			wait *= 2
			continue
		}

		log.Printf("summarizeSection: Gemini error for %q: %v", label, err)
		if len(text) > 2000 {
			return text[:2000] + "…[troncato]"
		}
		return text
	}
	return ""
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
