package area32

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"mensadb/tools/aitools"
	"mensadb/tools/env"
	"mensadb/tools/spatial"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"golang.org/x/sync/errgroup"
)

// maxResumeConcurrency limita le chiamate Gemini parallele durante il sync
// dei documenti scraped. 5 è un compromesso fra throughput e rate-limit Gemini.
const maxResumeConcurrency = 5

var ErrUnableToConnect = errors.New("ErrUnableToConnect")
var ErrInvalidCredentials = errors.New("ErrInvalidCredentials")

type Area32User struct {
	Id                 string
	ImageUrl           string
	Fullname           string
	ExpireDate         time.Time
	IsMembershipActive bool
	IsATestMaker       bool
}

func (u *Area32User) IsExpired() bool {
	return time.Now().After(u.ExpireDate)
}

type ScraperApi struct {
	client   *resty.Client
	jar      *cookiejar.Jar
	email    string
	password string
}

func NewAPI() *ScraperApi {
	cookieJar, _ := cookiejar.New(nil)

	// Dialer IPv4-only: il container in produzione non ha route IPv6 funzionante;
	// con dual-stack happy-eyeballs il client Go si pianta sul tentativo v6 e
	// scade il timeout di 30s, mentre curl con fallback v4 risponde in 150ms.
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConnsPerHost:   2,
	}

	client := resty.New().
		SetTimeout(30 * time.Second).
		SetCookieJar(cookieJar).
		SetDoNotParseResponse(true).
		SetTransport(transport).
		// Header browser-like: cloud32 e` dietro Azure Front Door che applica
		// regole WAF a client minimali.
		SetHeader("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36").
		SetHeader("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8").
		SetHeader("Accept-Language", "it-IT,it;q=0.9,en;q=0.8")

	// Stoppa il redirect-following: il POST di login risponde 302 e vogliamo
	// vedere Set-Cookie / Location della risposta originale, non quella della
	// pagina finale. http.ErrUseLastResponse e` il sentinel "stop ma nessun
	// errore" di Go (vs. resty.NoRedirectPolicy che ritorna un errore generico).
	client.GetClient().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &ScraperApi{client: client, jar: cookieJar}
}

func (api *ScraperApi) DoLoginAndRetrieveMain(email, password string) (*Area32User, error) {
	api.email = email
	api.password = password
	resp, err := api.client.R().Get("https://www.cloud32.it/Associazioni/utenti/login?codass=170734")
	if err != nil {
		log.Println("area32 login: GET /login failed:", err)
		return nil, ErrUnableToConnect
	}
	loginGetRaw, _ := io.ReadAll(resp.RawBody())
	log.Printf("area32 login: GET /login status=%d len=%d", resp.StatusCode(), len(loginGetRaw))

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(loginGetRaw))
	if err != nil {
		return nil, err
	}

	var token string
	doc.Find("input").Each(func(i int, s *goquery.Selection) {
		if name, _ := s.Attr("name"); name == "_token" {
			token, _ = s.Attr("value")
		}
	})
	log.Printf("area32 login: csrf token present=%v len=%d", token != "", len(token))

	formData := map[string]string{
		"email":    email,
		"password": password,
		"_token":   token,
	}
	postResp, err := api.client.R().
		SetHeader("Referer", "https://www.cloud32.it/Associazioni/utenti/login?codass=170734").
		SetHeader("Origin", "https://www.cloud32.it").
		SetFormData(formData).
		Post("https://www.cloud32.it/Associazioni/utenti/login")
	if err != nil {
		log.Println("area32 login: POST /login failed:", err)
		return nil, ErrUnableToConnect
	}
	postRaw, _ := io.ReadAll(postResp.RawBody())
	log.Printf("area32 login: POST /login status=%d len=%d location=%q",
		postResp.StatusCode(), len(postRaw), postResp.Header().Get("Location"))
	for _, sc := range postResp.Header().Values("Set-Cookie") {
		name := sc
		if i := strings.Index(sc, "="); i > 0 {
			name = sc[:i]
		}
		log.Printf("area32 login: POST Set-Cookie: %s (full len=%d)", name, len(sc))
	}
	homeURL, _ := url.Parse("https://www.cloud32.it/Associazioni/utenti/home")
	if homeURL != nil {
		jarCookies := api.jar.Cookies(homeURL)
		names := make([]string, 0, len(jarCookies))
		for _, c := range jarCookies {
			names = append(names, c.Name)
		}
		log.Printf("area32 login: jar would send to /home %d cookies: %v", len(jarCookies), names)
	}
	// Parse ogni Set-Cookie del POST per vedere Path/Domain/Attributes
	for _, sc := range postResp.Header().Values("Set-Cookie") {
		// log primi 60 caratteri + attributi (path/domain) — niente valore raw per privacy
		parts := strings.Split(sc, ";")
		head := parts[0]
		if i := strings.Index(head, "="); i > 0 {
			head = head[:i] + "=<val>"
		}
		attrs := []string{}
		for _, p := range parts[1:] {
			attrs = append(attrs, strings.TrimSpace(p))
		}
		log.Printf("area32 login: cookie attrs %s | %s", head, strings.Join(attrs, " ; "))
	}

	resp, err = api.client.R().
		SetHeader("Referer", "https://www.cloud32.it/Associazioni/utenti/login?codass=170734").
		Get("https://www.cloud32.it/Associazioni/utenti/home")
	if err != nil {
		log.Println("area32 login: GET /home failed:", err)
		return nil, ErrUnableToConnect
	}
	homeRaw, _ := io.ReadAll(resp.RawBody())
	log.Printf("area32 login: GET /home status=%d len=%d hasPwInput=%v hasTessera=%v",
		resp.StatusCode(), len(homeRaw),
		bytes.Contains(bytes.ToLower(homeRaw), []byte(`type="password"`)),
		bytes.Contains(homeRaw, []byte("Tessera:")))

	doc, err = goquery.NewDocumentFromReader(bytes.NewReader(homeRaw))
	if err != nil {
		return nil, ErrUnableToConnect
	}

	imageUrl := retrieveImageUrl(doc)
	userId := retrieveID(doc)
	if userId == "" {
		// snippet leggibile per debug — non logga email/password
		head := homeRaw
		if len(head) > 400 {
			head = head[:400]
		}
		log.Printf("area32 login: empty userId, home head 400 chars: %q", string(head))
		return nil, ErrInvalidCredentials
	}
	expireDate := retrieveExpireDate(doc)
	fullName := retrieveFullName(doc)
	isMembershipActive := checkIsMembershipActive(doc)
	isTestMaker := checkIsTestMaker(doc)
	return &Area32User{
		Id:                 userId,
		ImageUrl:           imageUrl,
		ExpireDate:         expireDate,
		Fullname:           fullName,
		IsMembershipActive: isMembershipActive,
		IsATestMaker:       isTestMaker,
	}, nil
}

func retrieveImageUrl(doc *goquery.Document) string {
	foundImage := false
	imageUrl := ""
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if alt, _ := s.Attr("alt"); alt == "Foto" {
			if altImage, _ := s.Attr("src"); altImage != "" {
				foundImage = true
				imageUrl, _ = s.Attr("src")
			}
		}
	})

	if !foundImage {
		return ""
	}

	return "https://www.cloud32.it" + imageUrl
}

func retrieveID(doc *goquery.Document) string {
	foundID := false
	id := ""
	doc.Find("div").Each(func(i int, s *goquery.Selection) {
		if class, _ := s.Attr("class"); class == "col-sm-12" {
			if strings.Contains(s.Text(), "Tessera:") {
				s.Find("label").Each(func(i int, s *goquery.Selection) {
					id = s.Text()
					foundID = true
				})
			}
		}
	})

	if !foundID {
		return ""
	}

	return id
}

func retrieveExpireDate(doc *goquery.Document) time.Time {
	expireDate := time.Now().Add(time.Hour * 24 * 365 * 10)
	doc.Find("div").Each(func(i int, s *goquery.Selection) {
		if class, _ := s.Attr("class"); class == "col-sm-12" {
			if strings.Contains(s.Text(), "Scadenza:") {
				s.Find("label").Each(func(i int, s *goquery.Selection) {
					loc, _ := time.LoadLocation("Europe/Rome")
					expireDate, _ = time.ParseInLocation("02/01/2006", s.Text(), loc)
				})
			}
		}
	})
	return expireDate
}

func retrieveFullName(doc *goquery.Document) string {
	fullName := ""
	doc.Find("span").Each(func(i int, s *goquery.Selection) {
		if class, _ := s.Attr("class"); class == "itemless nomeprofilo" {
			fullName = s.Text()
		}
	})
	return fullName
}

func checkIsMembershipActive(doc *goquery.Document) bool {
	res := false
	doc.Find("li").Each(func(i int, s *goquery.Selection) {
		s.Find("a").Each(func(i int, s *goquery.Selection) {
			if strings.Contains(strings.ToLower(s.Text()), "registro soci") {
				res = true
			}
		})
	})
	return res
}

func checkIsTestMaker(doc *goquery.Document) bool {
	res := false
	doc.Find("li").Each(func(i int, s *goquery.Selection) {
		s.Find("a").Each(func(i int, s *goquery.Selection) {
			if strings.Contains(strings.ToLower(s.Text()), "test") {
				res = true
			}
		})
	})
	return res
}

func (api *ScraperApi) GetDocumentByPage(page int) ([]map[string]any, error) {
	resp, err := api.client.R().
		Get("https://www.cloud32.it/Associazioni/utenti/documenti/docs?page=" + strconv.Itoa(page))

	if err != nil {

		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(resp.RawBody())
	if err != nil {
		return nil, err
	}
	var documents []map[string]any
	doc.Find("table").Each(func(i int, s *goquery.Selection) {
		s.Find("tr").Each(func(i int, s *goquery.Selection) {
			if i == 0 {
				return
			}
			var document = make(map[string]any)
			s.Find("td").Each(func(i int, s *goquery.Selection) {
				switch i {
				case 0:
					if s.Text() == "" {
						document["date"] = nil
					} else {
						loc, _ := time.LoadLocation("Europe/Rome")
						date, _ := time.ParseInLocation("02/01/2006", s.Text(), loc)
						document["date"] = date
					}
				case 1:
					document["description"] = s.Text()
				case 4:
					document["image"] = "https://www.cloud32.it" + s.Find("img").AttrOr("src", "")
					document["link"] = "https://www.cloud32.it" + s.Find("a").AttrOr("href", "")
				case 6:
					document["dimension"] = s.Text()
				}
			})
			documents = append(documents, document)
		})
	})
	return documents, nil
}

func (api *ScraperApi) GetAllDocuments(app core.App, excludedUID []string) ([]map[string]any, error) {
	var documents []map[string]any
	for i := 1; ; i++ {
		pageDocuments, err := api.GetDocumentByPage(i)
		if err != nil {
			return nil, err
		}
		if len(pageDocuments) == 0 {
			break
		}
		documents = append(documents, pageDocuments...)
	}
	documents = invertArray(documents)

	// Pre-filtra i documenti da processare mantenendo l'indice originale
	// per preservare l'ordinamento finale.
	type pendingDoc struct {
		idx int
		doc map[string]any
	}
	var pending []pendingDoc
	for i, document := range documents {
		uid := uuid.NewMD5(uuid.MustParse(env.GetDocsUUID()), []byte(document["link"].(string))).String()
		if !slices.Contains(excludedUID, uid) {
			pending = append(pending, pendingDoc{idx: i, doc: document})
		}
	}

	// Fan-out controllato: download + resume Gemini in parallelo (max maxResumeConcurrency).
	// Errori isolati per singolo documento: se Gemini fallisce/timeout salviamo
	// resume="" e l'errore non blocca il batch — il riassunto verrà rigenerato
	// al prossimo sync.
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(maxResumeConcurrency)
	var mu sync.Mutex
	processed := make(map[int]map[string]any, len(pending))

	for _, p := range pending {
		p := p
		g.Go(func() error {
			if err := ctx.Err(); err != nil {
				return nil
			}
			fs, err := api.DownloadFile(p.doc["link"].(string))
			if err != nil {
				log.Println("Error downloading file:", err)
				return nil
			}
			p.doc["file"] = fs
			// ResumeDocument non propaga errori: in caso di fallimento Gemini
			// ritorna stringa vuota e logga internamente.
			p.doc["resume"] = aitools.ResumeDocument(app, fs)
			mu.Lock()
			processed[p.idx] = p.doc
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	// Ricostruisce il risultato preservando l'ordine originale.
	var resultDocuments []map[string]any
	for _, p := range pending {
		if d, ok := processed[p.idx]; ok {
			resultDocuments = append(resultDocuments, d)
		}
	}
	return resultDocuments, nil
}

func invertArray(arr []map[string]any) []map[string]any {
	for i, j := 0, len(arr)-1; i < j; i, j = i+1, j-1 {
		arr[i], arr[j] = arr[j], arr[i]
	}
	return arr
}

func (api *ScraperApi) DownloadFileNoError(url string) *filesystem.File {
	file, err := api.DownloadFile(url)
	if err != nil {
		log.Println("Error downloading file:", err)
		return nil
	}
	return file
}

func (api *ScraperApi) DownloadFile(url string) (*filesystem.File, error) {
	resp, err := api.client.R().Head(url)
	if err != nil {
		return nil, err
	}
	fileName := resp.Header().Get("content-disposition")
	if fileName == "" {
		fileName = "filedownloaded"
	} else {
		fileName = strings.Split(fileName, "filename=")[1]
		fileName = strings.ReplaceAll(fileName, `"`, "")
	}
	resp, err = api.client.R().Get(url)
	if err != nil {
		return nil, err
	}
	all, err := io.ReadAll(resp.RawBody())
	if err != nil {
		return nil, err
	}
	file, err := filesystem.NewFileFromBytes(all, fileName)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (api *ScraperApi) GetAllRegSoci() ([]map[string]any, error) {
	// NOTE: la sorgente non espone il numero totale di pagine, quindi non possiamo
	// scaricare le pagine in parallelo a priori (servirebbe sapere quante sono).
	// La parallelizzazione vera è dentro `GetRegSoci`, dove per ogni riga della
	// pagina facciamo download immagine + deep data: ora vengono fetchate in
	// parallelo con un semaforo limitato.
	// TODO: parallelizzare anche le pagine se in futuro l'HTML esporrà il totale.
	var allUsers []map[string]any
	i := 1
	for ; ; i++ {
		log.Println("Fetching members registry page:", i)
		users, err := api.GetRegSoci(i, "")
		if err != nil {
			return nil, err
		}
		if len(users) == 0 {
			break
		}
		allUsers = append(allUsers, users...)
	}
	log.Println("Fetched members:", len(allUsers))
	log.Println("Total pages fetched:", i)
	return allUsers, nil
}

func (api *ScraperApi) GetRegSoci(page int, search string) ([]map[string]any, error) {
	nameToSearch := ""
	surnameToSearch := ""
	if strings.Contains(search, " ") {
		parts := strings.SplitN(search, " ", 2)
		surnameToSearch = parts[0]
		nameToSearch = parts[1]
	} else {
		nameToSearch = search
		surnameToSearch = ""
	}

	visitedIds := map[string]bool{}
	var users []map[string]any
	var usersMu sync.Mutex

	parseTable := func(doc *goquery.Document) {
		// Prima passata: raccogli i dati "leggeri" (solo parsing HTML).
		type rowData struct {
			id        string
			name      string
			birthDate time.Time
			city      string
			state     string
			imgSrc    string
			link      string
		}
		var rows []rowData
		doc.Find("table").First().Find("tr").Each(func(i int, s *goquery.Selection) {
			if i == 0 {
				return
			}
			tds := s.Find("td")
			if tds.Length() < 7 {
				return
			}
			id := strings.TrimSpace(tds.Eq(1).Text())
			if visitedIds[id] {
				return
			}
			visitedIds[id] = true

			birthDateStr := strings.TrimSpace(tds.Eq(3).Text())
			loc, _ := time.LoadLocation("Europe/Rome")
			var birthDate time.Time
			birthDate, _ = time.ParseInLocation("02/01/2006", birthDateStr, loc)

			imgSrc, _ := tds.Eq(0).Find("img").Attr("src")
			link, _ := tds.Eq(6).Find("a").Attr("href")

			rows = append(rows, rowData{
				id:        id,
				name:      strings.TrimSpace(tds.Eq(2).Text()),
				birthDate: birthDate,
				city:      strings.TrimSpace(tds.Eq(4).Text()),
				state:     strings.TrimSpace(tds.Eq(5).Text()),
				imgSrc:    imgSrc,
				link:      link,
			})
		})

		if len(rows) == 0 {
			return
		}

		// Seconda passata: per ogni riga, scarica immagine + deep data in
		// parallelo con un semaforo molto stretto (max 2 concorrenti). Limite
		// piu` alto fa scattare l'anti-abuse di cloud32 che invalida la sessione
		// e ritorna HTML di login alle pagine successive, troncando la paginazione.
		g, ctx := errgroup.WithContext(context.Background())
		sem := make(chan struct{}, 2)
		fetched := make([]map[string]any, len(rows))

		for idx, r := range rows {
			idx, r := idx, r
			g.Go(func() error {
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return ctx.Err()
				}
				defer func() { <-sem }()

				image := api.DownloadFileNoError("https://www.cloud32.it" + r.imgSrc)
				deepData := api.GetRegSocioDeepData("https://www.cloud32.it" + r.link)

				fetched[idx] = map[string]any{
					"uid":               r.id,
					"name":              r.name,
					"birthDate":         r.birthDate,
					"city":              r.city,
					"state":             r.state,
					"area":              spatial.CheckProvinceFromState(r.state),
					"image":             image,
					"linkToFullProfile": "https://www.cloud32.it" + r.link,
					"deepData":          deepData,
					"full_profile_link": "https://www.cloud32.it" + r.link,
				}
				return nil
			})
		}
		// errgroup non dovrebbe mai fallire qui (i fetch sono best-effort),
		// ma in caso di ctx cancellato logghiamo e continuiamo con quanto raccolto.
		if err := g.Wait(); err != nil {
			log.Println("GetRegSoci parallel fetch warning:", err)
		}

		usersMu.Lock()
		for _, u := range fetched {
			if u != nil {
				users = append(users, u)
			}
		}
		usersMu.Unlock()
	}
	makeRequest := func(name, surname string) {
		url := "https://www.cloud32.it/Associazioni/utenti/regsocio?s_cognome=" + surname +
			"&s_nome=" + name + "&s_citta=&s_provincia=&s_regione=&Ricerca=Ricerca&page=" + strconv.Itoa(page)

		fetchAndParse := func() {
			resp, err := api.client.R().Get(url)
			if err != nil {
				return
			}
			raw, err := io.ReadAll(resp.RawBody())
			if err != nil {
				return
			}
			// Cloud32 a volte risponde con la pagina di login quando la sessione
			// scade (anti-abuse dopo molte richieste). Detect e relogin una volta.
			if bytes.Contains(bytes.ToLower(raw), []byte(`type="password"`)) {
				log.Println("area32: session expired mid-scrape, re-login")
				if api.email == "" || api.password == "" {
					return
				}
				if _, err := api.DoLoginAndRetrieveMain(api.email, api.password); err != nil {
					log.Println("area32: relogin failed:", err)
					return
				}
				resp, err = api.client.R().Get(url)
				if err != nil {
					return
				}
				raw, err = io.ReadAll(resp.RawBody())
				if err != nil {
					return
				}
			}
			doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
			if err != nil {
				return
			}
			parseTable(doc)
		}
		fetchAndParse()
	}

	makeRequest(nameToSearch, surnameToSearch)
	if surnameToSearch != "" && surnameToSearch != nameToSearch {
		makeRequest(surnameToSearch, nameToSearch)
	}

	// Sort alphabetically by name
	sort.Slice(users, func(i, j int) bool {
		return users[i]["name"].(string) < users[j]["name"].(string)
	})

	return users, nil
}

// GetRegSocioDeepData retrieves deep data for a member from their full profile page
func (api *ScraperApi) GetRegSocioDeepData(url string) map[string]string {
	resp, err := api.client.R().Get(url)
	if err != nil {
		return map[string]string{}
	}

	doc, err := goquery.NewDocumentFromReader(resp.RawBody())
	if err != nil {
		return map[string]string{}
	}

	data := map[string]string{}
	doc.Find(".form-group").Each(func(i int, s *goquery.Selection) {
		key := strings.TrimSpace(s.Find("div").First().Text())
		value := strings.TrimSpace(s.Find("label").Last().Text())
		if value == "" {
			value, _ = s.Find("a").Last().Attr("href")
		}
		if key != "" && value != "" {
			data[key] = value
		}
	})

	return data
}
