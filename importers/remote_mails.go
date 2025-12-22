package importers

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
)

type Forwarding struct {
	Enabled string   `xml:"enabled" json:"enabled"`
	Address []string `xml:"address" json:"address"`
}

type MailName struct {
	Id         string     `xml:"id" json:"id"`
	Name       string     `xml:"name" json:"name"`
	Forwarding Forwarding `xml:"forwarding" json:"forwarding"`
}

type MailEntry struct {
	Status   string   `xml:"status" json:"status"`
	MailName MailName `xml:"mailname" json:"mailname"`
}

type MailInfo struct {
	Result []MailEntry `xml:"result" json:"result"`
}

type Mail struct {
	MailInfo MailInfo `xml:"get_info" json:"get_info"`
}

type Container struct {
	Mail Mail `xml:"mail" json:"mail"`
}

func GetFullMailList() {

	const myurl = "https://michael.mensaitalia.it:8443/enterprise/control/agent.php"
	const xmlbody = `<?xml version="1.0" encoding="UTF-8"?><packet><mail><get_info><filter><site-id>2</site-id></filter><forwarding /></get_info></mail></packet>`

	request, error := http.NewRequest("POST", myurl, strings.NewReader(xmlbody))
	if error != nil {
		log.Fatal(error)
	}
	request.Header.Set("Content-Type", "text/xml; charset=UTF-8")
	request.Header.Set("HTTP_AUTH_LOGIN", "dev")
	request.Header.Set("HTTP_AUTH_PASSWD", "ygpmbUzcwQGZ")

	client := &http.Client{}
	resp, err := client.Do(request)

	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	xmlResult, err := io.ReadAll(resp.Body)
	var container Container
	_ = xml.Unmarshal([]byte(xmlResult), &container)
	bt, _ := json.Marshal(container)

	//store into file
	os.WriteFile("mails.json", bt, 0644)
}

func ReadFromJson() Container {
	jsonFile, err := os.Open("mails.json")
	if err != nil {
		if os.IsNotExist(err) {
			GetFullMailList()
			return ReadFromJson()
		}
	}
	defer jsonFile.Close()

	var container Container
	jsonParser := json.NewDecoder(jsonFile)
	jsonParser.Decode(&container)

	return container
}

func RetrieveForwardedMail(name string, alreadyChecked ...string) (res []string) {
	container := ReadFromJson()
	for _, mailEntry := range container.Mail.MailInfo.Result {
		if mailEntry.MailName.Name == name {
			if slices.Contains(alreadyChecked, name) {
				return
			}
			for _, address := range mailEntry.MailName.Forwarding.Address {
				if !strings.Contains(address, "@mensa.it") {
					continue
				} else {
					res = append(res, address)
					res = append(res, RetrieveForwardedMail(strings.Split(address, "@")[0], append(alreadyChecked, name)...)...)
				}
			}
		}
	}
	return res
}

func RetrieveAliasFromMail(mail string) (res string) {
	container := ReadFromJson()
	for _, mailEntry := range container.Mail.MailInfo.Result {
		for _, address := range mailEntry.MailName.Forwarding.Address {
			if strings.TrimSpace(strings.ToLower(address)) == strings.TrimSpace(strings.ToLower(mail)) {
				return mailEntry.MailName.Name + "@mensa.it"
			}
		}
	}
	return ""
}
