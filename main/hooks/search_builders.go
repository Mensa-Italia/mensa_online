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
	region := ""
	officeName := ""
	if oid := rec.GetString("local_office"); oid != "" {
		if o, err := app.FindRecordById("local_offices", oid); err == nil {
			region = o.GetString("region")
			officeName = o.GetString("name")
		}
	}
	body := joinNonEmpty(" ", rec.GetString("description"), officeName, region)
	tags := filterNonEmpty(rec.GetString("group_type"), region)
	return search.Doc{
		ID:         rec.Id,
		Type:       "sig",
		Title:      rec.GetString("name"),
		Body:       body,
		Tags:       tags,
		Region:     region,
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

// BuildLocalOfficeDoc indicizza un gruppo locale (Lombardia, Lazio, ecc.).
// Tag = regione, body = name + region + bio. Visibility "members" (gli
// utenti devono essere autenticati per vedere il dettaglio dell'ufficio).
func BuildLocalOfficeDoc(app core.App, rec *core.Record) search.Doc {
	region := rec.GetString("region")
	body := joinNonEmpty(" ", rec.GetString("name"), region, rec.GetString("bio"))
	return search.Doc{
		ID:         rec.Id,
		Type:       "local_office",
		Title:      rec.GetString("name"),
		Body:       body,
		Tags:       filterNonEmpty(region),
		Region:     region,
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

// BuildLocalOfficeAdminDoc indicizza un segretario o co-segretario di un
// gruppo locale. Title costruito come "Segretario di X" o "Co-segretario
// di X" cosi` cercando il nome del referente compare anche l'ufficio.
// Body raccoglie nome socio + email per matching full-text.
func BuildLocalOfficeAdminDoc(app core.App, rec *core.Record) search.Doc {
	officeName, region := lookupOfficeNameRegion(app, rec.GetString("local_office"))
	memberName, memberMail := lookupMemberNameMail(app, rec.GetString("user"))

	roleLabel := "Co-segretario"
	if rec.GetBool("is_the_officer") {
		roleLabel = "Segretario"
	}
	title := roleLabel
	if officeName != "" {
		title = roleLabel + " di " + officeName
	}
	body := joinNonEmpty(" ", memberName, memberMail, officeName, region, roleLabel)
	return search.Doc{
		ID:         rec.Id,
		Type:       "local_office_admin",
		Title:      title,
		Body:       body,
		Tags:       filterNonEmpty(region, roleLabel, officeName),
		Region:     region,
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

// BuildLocalOfficeTestAssistantDoc indicizza un assistente al test di un
// gruppo locale. Stessa logica di BuildLocalOfficeAdminDoc ma ruolo
// "Assistente al test".
func BuildLocalOfficeTestAssistantDoc(app core.App, rec *core.Record) search.Doc {
	officeName, region := lookupOfficeNameRegion(app, rec.GetString("local_office"))
	memberName, memberMail := lookupMemberNameMail(app, rec.GetString("user"))

	const roleLabel = "Assistente al test"
	title := roleLabel
	if officeName != "" {
		title = roleLabel + " di " + officeName
	}
	body := joinNonEmpty(" ", memberName, memberMail, officeName, region, roleLabel)
	return search.Doc{
		ID:         rec.Id,
		Type:       "local_office_test_assistant",
		Title:      title,
		Body:       body,
		Tags:       filterNonEmpty(region, roleLabel, officeName),
		Region:     region,
		Visibility: "members",
		CreatedAt:  rec.GetDateTime("created").Time(),
	}
}

// lookupOfficeNameRegion ritorna (name, region) di un local_office, o
// (\"\", \"\") se non trovato.
func lookupOfficeNameRegion(app core.App, officeID string) (string, string) {
	if officeID == "" {
		return "", ""
	}
	o, err := app.FindRecordById("local_offices", officeID)
	if err != nil || o == nil {
		return "", ""
	}
	return o.GetString("name"), o.GetString("region")
}

// lookupMemberNameMail risale al socio collegato a un users.id, leggendo
// nome e alias_mail da members_registry (entry autoritativa per
// l'anagrafica). Se l'utente non ha record in members_registry, ripiega
// su users.name.
func lookupMemberNameMail(app core.App, userID string) (string, string) {
	if userID == "" {
		return "", ""
	}
	if mr, err := app.FindRecordById("members_registry", userID); err == nil && mr != nil {
		return mr.GetString("name"), mr.GetString("alias_mail")
	}
	if u, err := app.FindRecordById("users", userID); err == nil && u != nil {
		return u.GetString("name"), u.GetString("email")
	}
	return "", ""
}

// BuildLinktreeLinkDoc indicizza un singolo link del linktree di un gruppo
// locale come tipo "linktree_link". Solo i record kind="link" e active=true
// vanno chiamati qui (il filtro lo fa il caller / hook). Pubblico, non
// member-only: i linktree dei gruppi sono per definizione pagine pubbliche.
//
// Body include il nome del local_office e la regione per dare contesto:
// cercando "instagram lombardia" deve uscire il link Instagram della Lombardia.
func BuildLinktreeLinkDoc(app core.App, rec *core.Record) search.Doc {
	officeName := ""
	region := ""
	if oid := rec.GetString("local_office"); oid != "" {
		if o, err := app.FindRecordById("local_offices", oid); err == nil {
			officeName = o.GetString("name")
			region = o.GetString("region")
		}
	}
	body := joinNonEmpty(" ", rec.GetString("url"), officeName, region)
	tags := filterNonEmpty(region, officeName)

	return search.Doc{
		ID:         rec.Id,
		Type:       "linktree_link",
		Title:      rec.GetString("title"),
		Body:       body,
		Tags:       tags,
		Region:     region,
		Visibility: "public",
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

// BuildPodcastEpisodeDoc indicizza un singolo episodio. Body include
// description + transcript Gemini (se disponibile in podcast_episodes_transcript
// con duration_seconds > 0): cosi` cercare "intelligenza artificiale" trova
// l'episodio dove ne parlano per 20 minuti anche se il titolo non lo cita.
func BuildPodcastEpisodeDoc(app core.App, rec *core.Record) search.Doc {
	seriesTitle := ""
	if pid := rec.GetString("podcast"); pid != "" {
		if p, err := app.FindRecordById("podcasts", pid); err == nil {
			seriesTitle = p.GetString("title")
		}
	}

	transcript := ""
	if tr, err := app.FindFirstRecordByData("podcast_episodes_transcript", "episode", rec.Id); err == nil && tr != nil {
		if tr.GetInt("duration_seconds") > 0 {
			transcript = tr.GetString("transcript")
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
		Body:       joinNonEmpty(" ", rec.GetString("description"), transcript),
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
