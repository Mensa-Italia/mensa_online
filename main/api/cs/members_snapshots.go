package cs

import (
	"mensadb/tools/cdnfiles"

	"github.com/pocketbase/pocketbase/core"
)

func MembersSnapshotsHandler(e *core.RequestEvent) error {
	s3settings := e.App.Settings().S3
	listOfStuffs, err := cdnfiles.ListKeysInS3Prefix(e.App, s3settings.Bucket, "members_snapshots/")
	if err != nil {
		return err
	}

	type SnapshotInfo struct {
		History []string `json:"history"`
	}

	var snapshotInfo SnapshotInfo = SnapshotInfo{
		History: []string{},
	}

	for _, key := range listOfStuffs {
		snapshotInfo.History = append(snapshotInfo.History, "https://svc.mensa.it/api/cs/members-snapshots/"+key)
	}

	return e.JSON(200, listOfStuffs)
}

func MemberSnapshotByKeyHandler(e *core.RequestEvent) error {
	return e.String(200, "Not implemented yet")
}
