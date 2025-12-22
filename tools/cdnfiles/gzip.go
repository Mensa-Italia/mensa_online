package cdnfiles

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// GzipCompressJSON serializza v come JSON e lo comprime in gzip.
// Se fileName è valorizzato, viene usato come header "Name" del gzip.
func GzipCompressJSON(v any, fileName string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return GzipCompressBytes(b, fileName)
}

// GzipCompressBytes comprime bytes in formato gzip.
// Se fileName è valorizzato, viene usato come header "Name" del gzip.
func GzipCompressBytes(b []byte, fileName string) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if fileName != "" {
		zw.Name = fileName
	}
	zw.ModTime = time.Now()

	if _, err := zw.Write(b); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GzipDecompressBytes decomprime bytes gzip e ritorna il contenuto non compresso.
func GzipDecompressBytes(gz []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("gzip: open reader: %w", err)
	}
	defer func() { _ = zr.Close() }()

	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("gzip: read: %w", err)
	}
	return out, nil
}
