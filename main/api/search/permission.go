package search

import (
	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/dbtools"
)

// typeVisibility returns the static visibility and required_power for each type.
var typeVisibility = map[string]struct {
	visibility    string
	requiredPower string
}{
	"event":    {"public", ""},
	"sig":      {"public", ""},
	"deal":     {"members", ""},
	"document": {"members", ""},
	"member":    {"members", ""},
	"org_group":  {"members", ""},
	"org_role":   {"members", ""},
	"quid_article":    {"public", ""},
	"quid_issue":      {"public", ""},
	"podcast":         {"public", ""},
	"podcast_episode": {"public", ""},
	"linktree_link":   {"public", ""},
}

func allow(authUser *core.Record, visibility, requiredPower string) bool {
	if visibility == "public" {
		return true
	}
	if authUser == nil {
		return false
	}
	// members: any logged-in user with no required power passes (fase 1)
	if visibility == "members" {
		return dbtools.HasPower(authUser, requiredPower)
	}
	// restricted: must have the named power
	return dbtools.HasPower(authUser, requiredPower)
}
