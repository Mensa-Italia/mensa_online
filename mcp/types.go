package mcp

// searchableType descrive un tipo indicizzato in Bleve + la collection PB
// di origine. Sorgente unica di verita` per i tool MCP: ogni voce qui
// genera 1 tool di search per-type + 2 tool collection (list + get).
type searchableType struct {
	Key         string // identifier del tipo come usato in Bleve (es. "event")
	Collection  string // nome collection PB (es. "events")
	Singular    string // nome leggibile singolare (es. "event")
	Plural      string // nome leggibile plurale (es. "events")
	Description string // descrizione utile per il modello chiamante
}

// allTypes elenca tutti i tipi esposti via MCP. Aggiungere qui un tipo
// produce automaticamente search_X, list_X, get_X.
var allTypes = []searchableType{
	{Key: "event", Collection: "events", Singular: "event", Plural: "events",
		Description: "Mensa Italia events (national, regional, online). Key fields: name, description, when_start, when_end, position, is_national, is_public, owner, local_office."},
	{Key: "sig", Collection: "sigs", Singular: "group", Plural: "groups",
		Description: "Mensa Italia groups: special interest groups (SIG), local clubs, Facebook/Telegram/WhatsApp chats. Field group_type discriminates them. local_office may link territorial ones to a local office."},
	{Key: "deal", Collection: "deals", Singular: "deal", Plural: "deals",
		Description: "Member discounts and partnerships. Fields: name, details, commercial_sector, position, who, owner."},
	{Key: "document", Collection: "documents", Singular: "document", Plural: "documents",
		Description: "Official Mensa documents (bilanci, verbali, normativa, modulistica, ecc.). Fields: name, category, description, published, file. The related collection documents_elaborated stores AI-generated summaries (ia_resume)."},
	{Key: "member", Collection: "members_registry", Singular: "member", Plural: "members",
		Description: "Active Mensa Italia members (snapshot from Area32). Fields: name, city, state (region), area, alias_mail (@mensa.it), image. Only is_active=true members are listed."},
	{Key: "org_group", Collection: "org_chart_groups", Singular: "org_group", Plural: "org_groups",
		Description: "Groups of the national organigramma (e.g. Consiglio Direttivo, Segreteria, Commissioni). Fields: title, order."},
	{Key: "org_role", Collection: "org_chart_members", Singular: "org_role", Plural: "org_roles",
		Description: "Individual roles in the national organigramma. Fields: group (relation), user (relation to members_registry), role (text)."},
	{Key: "quid_issue", Collection: "quid_issues", Singular: "Quid issue", Plural: "Quid issues",
		Description: "Quid magazine issues (digital + PDF archive). Fields: number, name, slug, articles_count, image, published_at, pdf_url (for issues 1-12 that are only available as PDF)."},
	{Key: "quid_article", Collection: "quid_articles", Singular: "Quid article", Plural: "Quid articles",
		Description: "Individual articles of Quid magazine (issues 13+, sourced from WordPress)."},
	{Key: "podcast", Collection: "podcasts", Singular: "podcast", Plural: "podcasts",
		Description: "Mensa-related podcast series (auto-imported from YouTube playlists)."},
	{Key: "podcast_episode", Collection: "podcast_episodes", Singular: "podcast episode", Plural: "podcast episodes",
		Description: "Single episodes of imported podcasts. Audio is hosted on Minio. Fields: title, description, podcast, duration_seconds, published_at."},
	{Key: "linktree_link", Collection: "local_offices_links", Singular: "local office link", Plural: "local office links",
		Description: "Linktree-style links curated by each local office. Tree structure via parent + kind (section|link). Only active=true links are indexed."},
}

// typeByKey resolves Key → *searchableType.
func typeByKey(key string) *searchableType {
	for i := range allTypes {
		if allTypes[i].Key == key {
			return &allTypes[i]
		}
	}
	return nil
}


