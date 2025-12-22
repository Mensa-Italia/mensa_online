package cs

import (
	"encoding/json"
	"mensadb/tools/cdnfiles"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/tidwall/gjson"
)

func MembersSnapshotsHandler(e *core.RequestEvent) error {
	s3settings := e.App.Settings().S3
	listOfStuffs, err := cdnfiles.ListKeysInS3Prefix(e.App, s3settings.Bucket, "snapshot_members/")
	if err != nil {
		return err
	}

	type SnapshotInfoElem struct {
		Timestamp string `json:"timestamp"`
		URL       string `json:"url"`
	}

	type SnapshotInfo struct {
		History []SnapshotInfoElem `json:"history"`
	}

	var snapshotInfo SnapshotInfo = SnapshotInfo{
		History: []SnapshotInfoElem{},
	}

	for _, key := range listOfStuffs {
		key := strings.ReplaceAll(key, "snapshot_members/", "")
		timestamp := strings.ReplaceAll(key, ".json.gz", "")
		snapshotInfo.History = append(snapshotInfo.History, SnapshotInfoElem{
			Timestamp: timestamp,
			URL:       "https://svc.mensa.it/api/cs/members-snapshots/" + key,
		})
	}

	return e.JSON(200, snapshotInfo)
}

func MemberSnapshotByKeyHandler(e *core.RequestEvent) error {
	s3settings := e.App.Settings().S3
	key := e.Request.PathValue("key")
	hideNotActive := e.Request.URL.Query().Get("hideNotActive")
	hideNowNotActive := e.Request.URL.Query().Get("hideNowNotActive")

	fileContent, err := cdnfiles.RetrieveFileFromS3(e.App, s3settings.Bucket, "snapshot_members/"+key)

	if err != nil {
		return err
	}

	decompressedContent, err := cdnfiles.GzipDecompressBytes(fileContent)
	if err != nil {
		return err
	}
	filteredContent, err := filterMembersSnapshot(e.App, decompressedContent, hideNotActive != "", hideNowNotActive != "")
	if err != nil {
		return err
	}

	var jsonData interface{}
	err = json.Unmarshal([]byte(filteredContent), &jsonData)

	return e.JSON(200, jsonData)
}

func filterMembersSnapshot(app core.App, snapshotData []byte, hideNotActive bool, hideNowNotActive bool) (string, error) {
	snapshotElems := gjson.ParseBytes(snapshotData)
	records, _ := app.FindAllRecords("members_registry", dbx.NewExp("is_active = true"))
	activeMemberIds := map[string]bool{}
	for _, record := range records {
		activeMemberIds[record.Id] = true
	}

	snapshotElems.ForEach(func(key, value gjson.Result) bool {
		memberId := value.Get("id").String()
		isActive := value.Get("is_active").Bool()
		if hideNotActive && !isActive {
			return false // remove this element
		}
		if hideNowNotActive {
			if _, exists := activeMemberIds[memberId]; !exists {
				return false // remove this element
			}
		}
		return true // keep this element
	})

	//snapshotElems.String() to bytes

	return snapshotElems.String(), nil

}
