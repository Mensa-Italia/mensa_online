package aitools

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
)

// geminiSupportedMIMETypes lists MIME types accepted by the Gemini Files API.
var geminiSupportedMIMETypes = map[string]bool{
	"application/pdf":        true,
	"text/plain":             true,
	"text/html":              true,
	"text/css":               true,
	"text/javascript":        true,
	"text/csv":               true,
	"text/markdown":          true,
	"text/xml":               true,
	"text/rtf":               true,
	"image/png":              true,
	"image/jpeg":             true,
	"image/webp":             true,
	"image/heic":             true,
	"image/heif":             true,
	"image/gif":              true,
	"audio/wav":              true,
	"audio/mp3":              true,
	"audio/aiff":             true,
	"audio/aac":              true,
	"audio/ogg":              true,
	"audio/flac":             true,
	"video/mp4":              true,
	"video/mpeg":             true,
	"video/mov":              true,
	"video/avi":              true,
	"video/x-flv":            true,
	"video/mpg":              true,
	"video/webm":             true,
	"video/wmv":              true,
	"video/3gpp":             true,
}

// convertToTextFallback tries to extract readable text from unsupported file formats.
// Returns (textData, "text/plain", nil) on success, or an error when conversion is impossible.
func convertToTextFallback(data []byte, mimeType, name string) ([]byte, string, error) {
	base := baseMIME(mimeType)
	switch base {
	case "application/zip", "application/x-zip-compressed", "application/x-zip":
		text, err := extractZipText(data, name)
		if err != nil {
			return nil, "", err
		}
		return []byte(text), "text/plain", nil

	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		text, err := extractDocxText(data)
		if err != nil {
			return nil, "", err
		}
		return []byte(text), "text/plain", nil

	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		text, err := extractXlsxText(data)
		if err != nil {
			return nil, "", err
		}
		return []byte(text), "text/plain", nil

	case "application/x-ole-storage",
		"application/vnd.ms-excel",
		"application/msword",
		"application/vnd.ms-powerpoint":
		// Old binary OLE format (.doc, .xls, .ppt) — heuristic printable-text extraction.
		text := extractOLEPrintableText(data, name)
		return []byte(text), "text/plain", nil

	default:
		return nil, "", fmt.Errorf("no text converter for MIME type: %s", mimeType)
	}
}

func baseMIME(mimeType string) string {
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		return strings.TrimSpace(mimeType[:idx])
	}
	return mimeType
}

// extractZipText lists and extracts readable content from a ZIP archive.
func extractZipText(data []byte, zipName string) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Archivio ZIP: %s\n\n", zipName))

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		sb.WriteString(fmt.Sprintf("--- %s ---\n", f.Name))
		rc, err := f.Open()
		if err != nil {
			sb.WriteString("[errore apertura file]\n")
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			sb.WriteString("[errore lettura file]\n")
			continue
		}
		sb.WriteString(extractReadableContent(content, f.Name))
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// extractReadableContent picks the best text representation for raw bytes.
func extractReadableContent(data []byte, name string) string {
	lname := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lname, ".docx"):
		if text, err := extractDocxText(data); err == nil {
			return text
		}
	case strings.HasSuffix(lname, ".xlsx"):
		if text, err := extractXlsxText(data); err == nil {
			return text
		}
	case strings.HasSuffix(lname, ".txt"),
		strings.HasSuffix(lname, ".csv"),
		strings.HasSuffix(lname, ".md"),
		strings.HasSuffix(lname, ".html"),
		strings.HasSuffix(lname, ".xml"):
		if isPrintableUTF8(data) {
			return string(data)
		}
	}
	// Fallback: extract long runs of printable ASCII.
	printable := extractPrintableText(data)
	if len(printable) > 50 {
		return printable
	}
	return fmt.Sprintf("[file binario: %s]\n", name)
}

// extractDocxText reads word/document.xml from a DOCX (ZIP) and strips XML tags.
func extractDocxText(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range r.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return "", err
		}
		return stripXMLToText(content), nil
	}
	return "", fmt.Errorf("word/document.xml non trovato nel DOCX")
}

// extractXlsxText reads sheet XML files and shared strings from an XLSX archive.
func extractXlsxText(data []byte) (string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}

	sharedStrings := []string{}
	for _, f := range r.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		rc, _ := f.Open()
		content, _ := io.ReadAll(rc)
		rc.Close()
		sharedStrings = parseXLSXSharedStrings(content)
		break
	}

	var sb strings.Builder
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/sheet") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		sb.WriteString(fmt.Sprintf("=== Foglio: %s ===\n", f.Name))
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		sb.WriteString(parseXLSXSheet(content, sharedStrings))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

type xlsxSST struct {
	SI []struct {
		T string `xml:"t"`
		R []struct {
			T string `xml:"t"`
		} `xml:"r"`
	} `xml:"si"`
}

func parseXLSXSharedStrings(data []byte) []string {
	var sst xlsxSST
	_ = xml.Unmarshal(data, &sst)
	result := make([]string, len(sst.SI))
	for i, si := range sst.SI {
		if si.T != "" {
			result[i] = si.T
		} else {
			var parts []string
			for _, r := range si.R {
				parts = append(parts, r.T)
			}
			result[i] = strings.Join(parts, "")
		}
	}
	return result
}

type xlsxWorksheet struct {
	SheetData struct {
		Rows []struct {
			Cells []struct {
				V string `xml:"v"`
				T string `xml:"t,attr"`
			} `xml:"c"`
		} `xml:"row"`
	} `xml:"sheetData"`
}

func parseXLSXSheet(data []byte, sharedStrings []string) string {
	var ws xlsxWorksheet
	_ = xml.Unmarshal(data, &ws)

	var sb strings.Builder
	for _, row := range ws.SheetData.Rows {
		var cells []string
		for _, cell := range row.Cells {
			val := cell.V
			if cell.T == "s" {
				idx := 0
				fmt.Sscanf(val, "%d", &idx)
				if idx < len(sharedStrings) {
					val = sharedStrings[idx]
				}
			}
			cells = append(cells, val)
		}
		sb.WriteString(strings.Join(cells, "\t"))
		sb.WriteString("\n")
	}
	return sb.String()
}

var xmlTagRe = regexp.MustCompile(`<[^>]+>`)
var whitespaceRe = regexp.MustCompile(`\s+`)

func stripXMLToText(data []byte) string {
	text := xmlTagRe.ReplaceAllString(string(data), " ")
	text = whitespaceRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

// extractOLEPrintableText extracts legible runs of printable ASCII from old binary Office formats.
func extractOLEPrintableText(data []byte, name string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Documento: %s (formato binario OLE/legacy)\n\n", name))
	sb.WriteString(extractPrintableText(data))
	return sb.String()
}

// extractPrintableText collects contiguous runs of printable bytes (min length 5).
func extractPrintableText(data []byte) string {
	var sb strings.Builder
	run := make([]byte, 0, 64)
	for _, b := range data {
		if b >= 32 && b < 127 || b == '\t' || b == '\n' || b == '\r' {
			run = append(run, b)
		} else {
			if len(run) >= 5 {
				sb.Write(run)
				sb.WriteByte('\n')
			}
			run = run[:0]
		}
	}
	if len(run) >= 5 {
		sb.Write(run)
	}
	return sb.String()
}

func isPrintableUTF8(data []byte) bool {
	for _, r := range string(data) {
		if !unicode.IsPrint(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}
