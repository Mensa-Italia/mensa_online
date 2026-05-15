package hooks

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"mensadb/tools/search"
)

func BuildEventDoc(app core.App, rec *core.Record) search.Doc {
	ownerName := fetchUserName(app, rec.GetString("owner"))
	posCity, posState := resolvePositionLabel(app, rec.GetString("position"))

	body := joinNonEmpty(" ", rec.GetString("description"), ownerName, posCity, posState)
	tags := filterNonEmpty(posState)

	return search.Doc{
		ID:            rec.Id,
		Type:          "event",
		Title:         rec.GetString("name"),
		Body:          body,
		Tags:          tags,
		Region:        posState,
		Visibility:    "public",
		RequiredPower: "",
		CreatedAt:     rec.GetDateTime("created").Time(),
	}
}

func BuildSigDoc(app core.App, rec *core.Record) search.Doc {
	tags := filterNonEmpty(rec.GetString("group_type"))
	return search.Doc{
		ID:         rec.Id,
		Type:       "sig",
		Title:      rec.GetString("name"),
		Body:       rec.GetString("description"),
		Tags:       tags,
		Region:     "",
		Visibility: "public",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

func BuildDealDoc(app core.App, rec *core.Record) search.Doc {
	ownerName := fetchUserName(app, rec.GetString("owner"))
	posCity, posState := resolvePositionLabel(app, rec.GetString("position"))

	body := joinNonEmpty(" ", rec.GetString("details"), ownerName, posCity, posState)
	tags := filterNonEmpty(rec.GetString("commercial_sector"), rec.GetString("who"))

	return search.Doc{
		ID:         rec.Id,
		Type:       "deal",
		Title:      rec.GetString("name"),
		Body:       body,
		Tags:       tags,
		Region:     posState,
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

func BuildDocumentDoc(app core.App, rec *core.Record) search.Doc {
	uploaderName := fetchUserName(app, rec.GetString("uploaded_by"))
	body := joinNonEmpty(" ", rec.GetString("description"), loadIaResume(app, rec), uploaderName)
	tags := filterNonEmpty(rec.GetString("category"))

	return search.Doc{
		ID:         rec.Id,
		Type:       "document",
		Title:      rec.GetString("name"),
		Body:       body,
		Tags:       tags,
		Region:     "",
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

// BuildOrgGroupDoc indicizza un gruppo dell'organigramma come tipo
// "org_group". Body resta vuoto: il match avviene quasi solo sul title.
func BuildOrgGroupDoc(app core.App, rec *core.Record) search.Doc {
	return search.Doc{
		ID:         rec.Id,
		Type:       "org_group",
		Title:      rec.GetString("title"),
		Body:       "",
		Tags:       nil,
		Region:     "",
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

func BuildOrgRoleDoc(app core.App, rec *core.Record) search.Doc {
	// rec e` un org_chart_members. Risale a group + user per costruire un
	// titolo leggibile e un body cercabile.
	groupTitle := ""
	if gid := rec.GetString("group"); gid != "" {
		if g, err := app.FindRecordById("org_chart_groups", gid); err == nil {
			groupTitle = g.GetString("title")
		}
	}
	// org_chart_members.user adesso punta a members_registry.
	memberName := ""
	if mid := rec.GetString("user"); mid != "" {
		if mrec, err := app.FindRecordById("members_registry", mid); err == nil {
			memberName = mrec.GetString("name")
		}
	}

	role := rec.GetString("role")
	title := role
	if groupTitle != "" {
		title = role + " — " + groupTitle
	}

	return search.Doc{
		ID:         rec.Id,
		Type:       "org_role",
		Title:      title,
		Body:       memberName,
		Tags:       filterNonEmpty(groupTitle),
		Region:     "",
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

// BuildQuidIssueDoc indicizza un numero di Quid come risultato di search
// distinto dai singoli articoli. Title = nome del numero (es. "Quid 16 - La
// Fine"), body vuoto: il match avviene quasi solo sul titolo. Pubblico.
func BuildQuidIssueDoc(app core.App, rec *core.Record) search.Doc {
	createdAt := rec.GetDateTime("published_at").Time()
	if createdAt.IsZero() {
		createdAt = rec.GetDateTime("created").Time()
	}
	return search.Doc{
		ID:         rec.Id,
		Type:       "quid_issue",
		Title:      rec.GetString("name"),
		Body:       "",
		Tags:       nil,
		Region:     "",
		Visibility: "public",
		CreatedAt:  createdAt,
	}
}

// BuildQuidArticleDoc indicizza un articolo Quid (cache da WordPress).
// Visibility "public": Quid e` pubblicato online, niente restrizioni.
func BuildQuidArticleDoc(app core.App, rec *core.Record) search.Doc {
	body := joinNonEmpty(" ", rec.GetString("excerpt"), rec.GetString("body"))
	tags := filterNonEmpty(rec.GetString("category_name"))

	createdAt := rec.GetDateTime("published_at").Time()
	if createdAt.IsZero() {
		createdAt = rec.GetDateTime("created").Time()
	}

	return search.Doc{
		ID:         rec.Id,
		Type:       "quid_article",
		Title:      rec.GetString("title"),
		Body:       body,
		Tags:       tags,
		Region:     "",
		Visibility: "public",
		CreatedAt:  createdAt,
	}
}

// BuildPodcastDoc indicizza una serie podcast (playlist YT). Public.
func BuildPodcastDoc(app core.App, rec *core.Record) search.Doc {
	return search.Doc{
		ID:         rec.Id,
		Type:       "podcast",
		Title:      rec.GetString("title"),
		Body:       rec.GetString("description"),
		Tags:       nil,
		Region:     "",
		Visibility: "public",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

// BuildPodcastEpisodeDoc indicizza un singolo episodio.
func BuildPodcastEpisodeDoc(app core.App, rec *core.Record) search.Doc {
	// Tag con il titolo della serie per match tipo "Mensa Talks: X".
	seriesTitle := ""
	if pid := rec.GetString("podcast"); pid != "" {
		if p, err := app.FindRecordById("podcasts", pid); err == nil {
			seriesTitle = p.GetString("title")
		}
	}
	createdAt := rec.GetDateTime("published_at").Time()
	if createdAt.IsZero() {
		createdAt = rec.GetDateTime("created").Time()
	}
	return search.Doc{
		ID:         rec.Id,
		Type:       "podcast_episode",
		Title:      rec.GetString("title"),
		Body:       rec.GetString("description"),
		Tags:       filterNonEmpty(seriesTitle),
		Region:     "",
		Visibility: "public",
		CreatedAt:  createdAt,
	}
}

// BuildMemberDoc indicizza un socio dal members_registry (sync Area32).
// I record con is_active=false non vanno chiamati qui (l'hook li Delete).
func BuildMemberDoc(app core.App, rec *core.Record) search.Doc {
	body := joinNonEmpty(" ",
		rec.GetString("alias_mail"),
		rec.GetString("original_mail"),
		rec.GetString("city"),
		rec.GetString("area"),
	)
	return search.Doc{
		ID:         rec.Id,
		Type:       "member",
		Title:      rec.GetString("name"),
		Body:       body,
		Tags:       filterNonEmpty(rec.GetString("area")),
		Region:     rec.GetString("state"),
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

func fetchUserName(app core.App, userId string) string {
	if userId == "" {
		return ""
	}
	rec, err := app.FindRecordById("users", userId)
	if err != nil {
		return ""
	}
	if n := rec.GetString("name"); n != "" {
		return n
	}
	return rec.GetString("username")
}

func resolvePositionLabel(app core.App, positionId string) (city, state string) {
	if positionId == "" {
		return "", ""
	}
	rec, err := app.FindRecordById("positions", positionId)
	if err != nil {
		return "", ""
	}
	return rec.GetString("name"), rec.GetString("state")
}

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

func filterNonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func joinNonEmpty(sep string, vals ...string) string {
	return strings.Join(filterNonEmpty(vals...), sep)
}
