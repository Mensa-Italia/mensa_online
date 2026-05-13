package search

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/it"
	"github.com/blevesearch/bleve/v2/mapping"
)

func buildMapping() mapping.IndexMapping {
	im := bleve.NewIndexMapping()

	italianText := bleve.NewTextFieldMapping()
	italianText.Analyzer = "it"
	italianText.Store = false

	keywordStored := bleve.NewTextFieldMapping()
	keywordStored.Analyzer = keyword.Name
	keywordStored.Store = true

	keywordField := bleve.NewTextFieldMapping()
	keywordField.Analyzer = keyword.Name
	keywordField.Store = false

	dateField := bleve.NewDateTimeFieldMapping()
	dateField.Store = false

	doc := bleve.NewDocumentMapping()
	doc.AddFieldMappingsAt("title", italianText)
	doc.AddFieldMappingsAt("body", italianText)
	doc.AddFieldMappingsAt("tags", italianText)
	doc.AddFieldMappingsAt("id", keywordStored)
	doc.AddFieldMappingsAt("type", keywordStored)
	doc.AddFieldMappingsAt("region", keywordField)
	doc.AddFieldMappingsAt("visibility", keywordField)
	doc.AddFieldMappingsAt("required_power", keywordField)
	doc.AddFieldMappingsAt("updated_at", dateField)

	im.DefaultMapping = doc
	im.DefaultAnalyzer = "it"
	return im
}
