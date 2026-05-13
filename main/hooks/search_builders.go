package hooks

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/search"
)

func BuildEventDoc(app core.App, rec *core.Record) search.Doc {
	return search.Doc{
		ID:            rec.Id,
		Type:          "event",
		Title:         rec.GetString("name"),
		Body:          rec.GetString("description"),
		Tags:          nil,
		Region:        resolvePositionRegion(app, rec.GetString("position")),
		Visibility:    "public",
		RequiredPower: "",
		UpdatedAt:     rec.GetDateTime("updated").Time(),
	}
}

func BuildSigDoc(app core.App, rec *core.Record) search.Doc {
	var tags []string
	if gt := rec.GetString("group_type"); gt != "" {
		tags = []string{gt}
	}
	return search.Doc{
		ID:         rec.Id,
		Type:       "sig",
		Title:      rec.GetString("name"),
		Body:       rec.GetString("description"),
		Tags:       tags,
		Region:     "",
		Visibility: "public",
		UpdatedAt:  rec.GetDateTime("updated").Time(),
	}
}

func BuildDealDoc(app core.App, rec *core.Record) search.Doc {
	tags := filterNonEmpty(rec.GetString("commercial_sector"), rec.GetString("who"))
	return search.Doc{
		ID:         rec.Id,
		Type:       "deal",
		Title:      rec.GetString("name"),
		Body:       rec.GetString("details"),
		Tags:       tags,
		Region:     resolvePositionRegion(app, rec.GetString("position")),
		Visibility: "members",
		UpdatedAt:  rec.GetDateTime("updated").Time(),
	}
}

func BuildDocumentDoc(app core.App, rec *core.Record) search.Doc {
	body := rec.GetString("description") + " " + loadIaResume(app, rec)
	tags := filterNonEmpty(rec.GetString("category"))
	return search.Doc{
		ID:         rec.Id,
		Type:       "document",
		Title:      rec.GetString("name"),
		Body:       body,
		Tags:       tags,
		Region:     "",
		Visibility: "members",
		UpdatedAt:  rec.GetDateTime("updated").Time(),
	}
}

func BuildUserDoc(app core.App, rec *core.Record) search.Doc {
	return search.Doc{
		ID:         rec.Id,
		Type:       "user",
		Title:      rec.GetString("name"),
		Body:       "",
		Tags:       nil,
		Region:     resolvePositionRegion(app, rec.GetString("position")),
		Visibility: "members",
		UpdatedAt:  rec.GetDateTime("updated").Time(),
	}
}

// resolvePositionRegion fetches the positions record and returns its "state"
// field, which holds the Italian region (e.g. "Lombardia"). The positions
// collection has no "region" field — "state" is the region selector.
// Returns "" on any error or empty positionId.
func resolvePositionRegion(app core.App, positionId string) string {
	if positionId == "" {
		return ""
	}
	posRec, err := app.FindRecordById("positions", positionId)
	if err != nil {
		return ""
	}
	return posRec.GetString("state")
}

// loadIaResume returns the ia_resume text from the linked documents_elaborated
// record. Returns "" on any failure.
func loadIaResume(app core.App, docRec *core.Record) string {
	elaboratedId := docRec.GetString("elaborated")
	if elaboratedId == "" {
		return ""
	}
	elaborated, err := app.FindRecordById("documents_elaborated", elaboratedId)
	if err != nil {
		return ""
	}
	return elaborated.GetString("ia_resume")
}

// filterNonEmpty returns a slice of only the non-empty strings from vals.
// Returns nil if none remain.
func filterNonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
