package memberchanges

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"mensadb/tools/cdnfiles"
)

// BackfillConfigKey e' la chiave di configs che segna il backfill come
// fatto: "true" e Backfill non fa piu' niente.
const BackfillConfigKey = "local_group_changes_backfilled"

// snapshotPrefix e' la cartella S3 degli snapshot del registro, scritti da
// dbtools.SnapshotArea32Members: snapshot_members/AAAA-MM-GG_hh-mm-ss.json.gz.
const snapshotPrefix = "snapshot_members/"

// SnapshotSource e' da dove il backfill legge gli snapshot: S3 in
// produzione, un finto nei test.
type SnapshotSource interface {
	// Keys ritorna le chiavi degli snapshot.
	Keys() ([]string, error)
	// Get ritorna il contenuto (JSON, gia' decompresso) di uno snapshot.
	Get(key string) ([]byte, error)
}

// s3Source legge gli snapshot dal bucket di PocketBase, con gli stessi
// helper delle route /api/cs/members-snapshots.
type s3Source struct{ app core.App }

func (s s3Source) Keys() ([]string, error) {
	return cdnfiles.ListKeysOnlyInS3Prefix(s.app, s.app.Settings().S3.Bucket, snapshotPrefix)
}

func (s s3Source) Get(key string) ([]byte, error) {
	compressed, _, err := cdnfiles.RetrieveFileFromS3(s.app, s.app.Settings().S3.Bucket, key)
	if err != nil {
		return nil, err
	}
	return cdnfiles.GzipDecompressBytes(compressed)
}

// Un backfill alla volta: lo lanciano sia l'avvio sia il cron manuale.
var backfillMu sync.Mutex

// Backfill ricostruisce, una volta sola, la storia dei movimenti dagli
// snapshot del registro su S3. Vedi BackfillFrom.
func Backfill(app core.App) error {
	return BackfillFrom(app, s3Source{app: app})
}

// BackfillFrom ricostruisce la storia dei movimenti dagli snapshot di src:
// il primo di ogni giorno, in ordine, ognuno confrontato con il precedente
// e registrato con il giorno dello snapshot; l'ultimo si confronta anche con
// il registro di adesso (giorno di oggi). Il primo snapshot non scrive
// niente: non c'e' un prima. A lavoro finito la chiave
// local_group_changes_backfilled di configs vale "true" e le chiamate dopo
// non fanno niente. Se gli snapshot non si possono elencare (S3 spento o
// irraggiungibile) lo dice nel log e ritorna nil senza segnare niente: ci
// riprova al prossimo avvio. Uno snapshot che non si legge si salta.
func BackfillFrom(app core.App, src SnapshotSource) error {
	if !backfillMu.TryLock() {
		app.Logger().Info("[memberchanges] backfill gia' in corso")
		return nil
	}
	defer backfillMu.Unlock()

	if getConfig(app, BackfillConfigKey) == "true" {
		return nil
	}
	started := time.Now()

	keys, err := src.Keys()
	if err != nil {
		app.Logger().Warn("[memberchanges] backfill rimandato: snapshot S3 non disponibili", "err", err)
		return nil
	}
	days := firstOfEachDay(keys)
	app.Logger().Info("[memberchanges] backfill iniziato", "snapshots", len(days))

	var prev Snapshot
	used := 0
	for _, d := range days {
		raw, err := src.Get(d.key)
		if err != nil {
			app.Logger().Warn("[memberchanges] backfill: snapshot non leggibile, saltato", "key", d.key, "err", err)
			continue
		}
		next, err := DecodeSnapshot(raw)
		if err != nil {
			app.Logger().Warn("[memberchanges] backfill: snapshot non valido, saltato", "key", d.key, "err", err)
			continue
		}
		used++
		if prev != nil {
			if err := Record(app, Diff(prev, next), d.day); err != nil {
				return fmt.Errorf("backfill %s: %w", d.key, err)
			}
		}
		prev = next
	}

	if prev != nil {
		current, err := Capture(app)
		if err != nil {
			return fmt.Errorf("backfill, registro di adesso: %w", err)
		}
		if err := Record(app, Diff(prev, current), time.Now()); err != nil {
			return fmt.Errorf("backfill, registro di adesso: %w", err)
		}
	}

	if err := setConfig(app, BackfillConfigKey, "true"); err != nil {
		return err
	}
	app.Logger().Info("[memberchanges] backfill finito", "snapshots", used, "in", time.Since(started).String())
	return nil
}

type daySnapshot struct {
	key string
	day time.Time
}

// firstOfEachDay tiene il primo snapshot di ogni giorno (la chiave piu'
// piccola, il nome comincia con data e ora), in ordine di giorno. Le chiavi
// che non hanno la forma snapshot_members/AAAA-MM-GG_... si scartano. Il
// giorno dello snapshot e' quello scritto nel nome, a mezzanotte di Roma.
func firstOfEachDay(keys []string) []daySnapshot {
	first := map[string]string{}
	for _, key := range keys {
		name := strings.TrimPrefix(key, snapshotPrefix)
		if name == key || !strings.HasSuffix(name, ".json.gz") || len(name) < 10 {
			continue
		}
		date := name[:10]
		if _, err := time.Parse("2006-01-02", date); err != nil {
			continue
		}
		if old, ok := first[date]; !ok || key < old {
			first[date] = key
		}
	}
	dates := make([]string, 0, len(first))
	for date := range first {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	out := make([]daySnapshot, 0, len(dates))
	for _, date := range dates {
		d, _ := time.Parse("2006-01-02", date)
		// mezzogiorno UTC di quel giorno: Day lo porta alla mezzanotte di Roma
		// dello stesso giorno di calendario
		out = append(out, daySnapshot{key: first[date], day: Day(d.Add(12 * time.Hour))})
	}
	return out
}

// DecodeSnapshot legge uno snapshot del registro: l'array JSON dei record di
// members_registry come li scrive dbtools.SnapshotArea32Members.
func DecodeSnapshot(raw []byte) (Snapshot, error) {
	var rows []struct {
		ID       string `json:"id"`
		Area     string `json:"area"`
		IsActive bool   `json:"is_active"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("snapshot vuoto")
	}
	out := make(Snapshot, len(rows))
	for _, r := range rows {
		if r.ID == "" {
			continue
		}
		out[r.ID] = State{Area: r.Area, Active: r.IsActive, Name: r.Name}
	}
	return out, nil
}

// getConfig e setConfig sono dbtools.GetInternalConfig e SetInternalConfig:
// copiati perche' dbtools importa questo pacchetto (il sync del registro).
func getConfig(app core.App, key string) string {
	record, err := app.FindFirstRecordByData("configs", "key", key)
	if err != nil || record == nil {
		return ""
	}
	return record.GetString("value")
}

func setConfig(app core.App, key, value string) error {
	collection, err := app.FindCollectionByNameOrId("configs")
	if err != nil {
		return fmt.Errorf("configs: %w", err)
	}
	record, err := app.FindFirstRecordByData(collection.Id, "key", key)
	if err != nil || record == nil {
		record = core.NewRecord(collection)
		record.Set("key", key)
	}
	record.Set("value", value)
	if err := app.Save(record); err != nil {
		return fmt.Errorf("config %q: %w", key, err)
	}
	return nil
}
