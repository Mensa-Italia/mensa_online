package search

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

func buildSearchRequest(q string, f Filters, limit int) *bleve.SearchRequest {
	var primary query.Query
	if q == "" {
		primary = bleve.NewMatchAllQuery()
	} else {
		// Match esatto (analizzato con stemmer italiano): peso pieno.
		titleQ := bleve.NewMatchQuery(q)
		titleQ.SetField("title")
		titleQ.SetBoost(3.0)

		bodyQ := bleve.NewMatchQuery(q)
		bodyQ.SetField("body")
		bodyQ.SetBoost(1.0)

		tagsQ := bleve.NewMatchQuery(q)
		tagsQ.SetField("tags")
		tagsQ.SetBoost(1.5)

		// Match fuzzy (1 edit Levenshtein) per typo come "Montenari" -> "Montanari".
		// Prefix 1: il primo char deve matchare esatto, riduce falsi positivi.
		// Boost ridotti rispetto al match esatto: chi azzecca lo spelling
		// resta sempre in cima.
		titleFuzzy := bleve.NewMatchQuery(q)
		titleFuzzy.SetField("title")
		titleFuzzy.SetFuzziness(1)
		titleFuzzy.SetPrefix(1)
		titleFuzzy.SetBoost(1.5)

		bodyFuzzy := bleve.NewMatchQuery(q)
		bodyFuzzy.SetField("body")
		bodyFuzzy.SetFuzziness(1)
		bodyFuzzy.SetPrefix(1)
		bodyFuzzy.SetBoost(0.5)

		primary = bleve.NewDisjunctionQuery(titleQ, bodyQ, tagsQ, titleFuzzy, bodyFuzzy)
	}

	parts := []query.Query{primary}

	if len(f.Types) == 1 {
		t := bleve.NewTermQuery(f.Types[0])
		t.SetField("type")
		parts = append(parts, t)
	} else if len(f.Types) > 1 {
		terms := make([]query.Query, 0, len(f.Types))
		for _, ty := range f.Types {
			tq := bleve.NewTermQuery(ty)
			tq.SetField("type")
			terms = append(terms, tq)
		}
		parts = append(parts, bleve.NewDisjunctionQuery(terms...))
	}

	if f.Region != "" {
		r := bleve.NewTermQuery(f.Region)
		r.SetField("region")
		parts = append(parts, r)
	}

	var final query.Query
	if len(parts) == 1 {
		final = parts[0]
	} else {
		final = bleve.NewConjunctionQuery(parts...)
	}

	req := bleve.NewSearchRequest(final)
	req.Size = limit
	req.From = 0
	req.Fields = []string{"type"}
	req.SortBy([]string{"-_score", "-created_at"})
	return req
}
