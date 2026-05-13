package search

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/blevesearch/bleve/v2"
)

var (
	idx       bleve.Index
	mu        sync.RWMutex
	indexPath string
)

func Init(path string) error {
	mu.Lock()
	defer mu.Unlock()
	if idx != nil {
		return errors.New("search: already initialized")
	}
	indexPath = path
	if _, err := os.Stat(path); err == nil {
		opened, err := bleve.Open(path)
		if err != nil {
			return fmt.Errorf("search: open index: %w", err)
		}
		idx = opened
		return nil
	}
	created, err := bleve.New(path, buildMapping())
	if err != nil {
		return fmt.Errorf("search: create index: %w", err)
	}
	idx = created
	return nil
}

// Reset closes the index, removes the on-disk directory, and reopens it empty.
func Reset() error {
	mu.Lock()
	defer mu.Unlock()
	if indexPath == "" {
		return errors.New("search: Init has not been called")
	}
	if idx != nil {
		if err := idx.Close(); err != nil {
			return fmt.Errorf("search: close index: %w", err)
		}
		idx = nil
	}
	if err := os.RemoveAll(indexPath); err != nil {
		return fmt.Errorf("search: remove index dir: %w", err)
	}
	created, err := bleve.New(indexPath, buildMapping())
	if err != nil {
		return fmt.Errorf("search: recreate index: %w", err)
	}
	idx = created
	return nil
}

func Shutdown() error {
	mu.Lock()
	defer mu.Unlock()
	if idx == nil {
		return nil
	}
	err := idx.Close()
	idx = nil
	if err != nil {
		return fmt.Errorf("search: close index: %w", err)
	}
	return nil
}
