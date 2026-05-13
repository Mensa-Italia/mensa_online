package search

import (
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

var ErrNotInitialized = errors.New("search: index not initialized")

func Upsert(d Doc) error {
	mu.RLock()
	defer mu.RUnlock()
	if idx == nil {
		return ErrNotInitialized
	}
	if err := idx.Index(d.ID, d); err != nil {
		return fmt.Errorf("search: index doc %q: %w", d.ID, err)
	}
	return nil
}

func Delete(id string) error {
	mu.RLock()
	defer mu.RUnlock()
	if idx == nil {
		return ErrNotInitialized
	}
	if err := idx.Delete(id); err != nil {
		return fmt.Errorf("search: delete doc %q: %w", id, err)
	}
	return nil
}

func Query(q string, f Filters, limit int) ([]Result, error) {
	mu.RLock()
	defer mu.RUnlock()
	if idx == nil {
		return nil, ErrNotInitialized
	}
	req := buildSearchRequest(q, f, limit)
	res, err := idx.Search(req)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	out := make([]Result, 0, len(res.Hits))
	for _, hit := range res.Hits {
		t := ""
		if v, ok := hit.Fields["type"].(string); ok {
			t = v
		}
		out = append(out, Result{ID: hit.ID, Type: t, Score: hit.Score})
	}
	return out, nil
}

// CountByType returns the number of documents indexed for the given type.
// Type "" means count all documents.
func CountByType(t string) (uint64, error) {
	mu.RLock()
	defer mu.RUnlock()
	if idx == nil {
		return 0, ErrNotInitialized
	}

	var q query.Query
	if t == "" {
		q = bleve.NewMatchAllQuery()
	} else {
		tq := bleve.NewTermQuery(t)
		tq.SetField("type")
		q = tq
	}

	req := bleve.NewSearchRequestOptions(q, 0, 0, false)
	res, err := idx.Search(req)
	if err != nil {
		return 0, fmt.Errorf("search: count by type %q: %w", t, err)
	}
	return uint64(res.Total), nil
}
