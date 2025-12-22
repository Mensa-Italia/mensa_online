package cs

import (
	"mensadb/tools/cdnfiles"
	"strings"

	"github.com/pocketbase/pocketbase/core"
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
		timestamp := strings.ReplaceAll(key, ".json.gz", "")
		snapshotInfo.History = append(snapshotInfo.History, SnapshotInfoElem{
			Timestamp: timestamp,
			URL:       "https://svc.mensa.it/api/cs/members-snapshots/" + key,
		})
	}

	return e.JSON(200, listOfStuffs)
}

func MemberSnapshotByKeyHandler(e *core.RequestEvent) error {
	return e.String(200, "Not implemented yet")
}
