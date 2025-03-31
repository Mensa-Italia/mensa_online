package area32

import (
	"errors"
	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"io"
	"mensadb/tools/aipower"
	"mensadb/tools/env"
	"net/http/cookiejar"
	"slices"
	"strconv"
	"strings"
	"time"
)

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
	client *resty.Client
}

func NewAPI() *ScraperApi {
	cookieJar, _ := cookiejar.New(nil)
	client := resty.New().SetCookieJar(cookieJar).SetDoNotParseResponse(true)
	return &ScraperApi{client: client}
}

func (api *ScraperApi) DoLoginAndRetrieveMain(email, password string) (*Area32User, error) {
	resp, err := api.client.R().
		Get("https://www.cloud32.it/Associazioni/utenti/login?codass=170734")
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(resp.RawBody())

	if err != nil {
		return nil, err
	}

	var token string
	doc.Find("input").Each(func(i int, s *goquery.Selection) {
		if name, _ := s.Attr("name"); name == "_token" {
			token, _ = s.Attr("value")
		}
	})

	formData := map[string]string{
		"email":    email,
		"password": password,
		"_token":   token,
	}
	_, err = api.client.R().
		SetFormData(formData).
		Post("https://www.cloud32.it/Associazioni/utenti/login")
	if err != nil {
		return nil, err
	}

	resp, err = api.client.R().
		Get("https://www.cloud32.it/Associazioni/utenti/home")
	if err != nil {
		return nil, err
	}

	doc, err = goquery.NewDocumentFromReader(resp.RawBody())
	if err != nil {
		return nil, err
	}

	imageUrl := retrieveImageUrl(doc)
	userId := retrieveID(doc)
	expireDate := retrieveExpireDate(doc)
	fullName := retrieveFullName(doc)
	isMembershipActive := checkIsMembershipActive(doc)
	isTestMaker := checkIsTestMaker(doc)
	if userId == "" {
		return nil, errors.New("Invalid credentials")
	}
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

func (api *ScraperApi) GetAllDocuments(excludedUID []string) ([]map[string]any, error) {
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
		break
	}
	documents = invertArray(documents)
	resultDocuments := []map[string]any{}
	for i, document := range documents {
		uid := uuid.NewMD5(uuid.MustParse(env.GetDocsUUID()), []byte(document["link"].(string))).String()
		if !slices.Contains(excludedUID, uid) {
			fs, err := api.DownloadFile(document["link"].(string))
			if err != nil {
				return nil, err
			}
			documents[i]["file"] = fs
			documents[i]["resume"] = aipower.AskResume(fs)
			if err != nil {
				return nil, err
			}
			resultDocuments = append(resultDocuments, documents[i])
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
