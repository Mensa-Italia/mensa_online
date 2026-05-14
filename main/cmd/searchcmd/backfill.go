package searchcmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"mensadb/main/hooks"
	"mensadb/tools/search"
)

const batchSize = 500

type builder func(core.App, *core.Record) search.Doc

var DefaultCollections = []string{"events", "sigs", "deals", "documents", "members_registry", "org_chart_members"}

// filterFor restituisce un filtro PB opzionale per ogni collection durante
// il backfill. members_registry indicizza solo i soci attivi (is_active=true).
func filterFor(collection string) string {
	switch collection {
	case "members_registry":
		return "is_active=true"
	default:
		return ""
	}
}

// Run performs the backfill. Callable from the cobra command or from a cron job.
func Run(ctx context.Context, app core.App, collections []string, reset bool) error {
	if len(collections) == 0 {
		return fmt.Errorf("no collections specified")
	}

	if reset {
		if err := search.Reset(); err != nil {
			return fmt.Errorf("reset: %w", err)
		}
		_, _ = fmt.Fprintln(os.Stdout, "search index reset")
	}

	start := time.Now()
	totals := make(map[string]int, len(collections))
	var mu sync.Mutex

	eg, _ := errgroup.WithContext(ctx)
	eg.SetLimit(5)

	for _, col := range collections {
		col := col
		eg.Go(func() error {
			count, err := backfillCollection(app, col)
			if err != nil {
				return fmt.Errorf("backfill %s: %w", col, err)
			}
			mu.Lock()
			totals[col] = count
			mu.Unlock()
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(os.Stdout, "\nbackfill complete in %s\n", time.Since(start).Round(time.Millisecond))
	for _, col := range collections {
		_, _ = fmt.Fprintf(os.Stdout, "  %-12s %d records\n", col, totals[col])
	}
	return nil
}

func New(app core.App) *cobra.Command {
	var types string
	var reset bool

	cmd := &cobra.Command{
		Use:   "search-backfill",
		Short: "Re-index all records into the search index",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Run(cmd.Context(), app, parseTypes(types), reset)
		},
	}

	cmd.Flags().StringVar(&types, "types", strings.Join(DefaultCollections, ","), "comma-separated collection names")
	cmd.Flags().BoolVar(&reset, "reset", false, "wipe the index before backfill")
	return cmd
}

func parseTypes(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func resolveBuilder(collection string) builder {
	switch collection {
	case "events":
		return hooks.BuildEventDoc
	case "sigs":
		return hooks.BuildSigDoc
	case "deals":
		return hooks.BuildDealDoc
	case "documents":
		return hooks.BuildDocumentDoc
	case "members_registry":
		return hooks.BuildMemberDoc
	case "org_chart_members":
		return hooks.BuildOrgRoleDoc
	default:
		return nil
	}
}

func backfillCollection(app core.App, collection string) (int, error) {
	build := resolveBuilder(collection)
	if build == nil {
		return 0, fmt.Errorf("no builder for collection %q", collection)
	}

	filter := filterFor(collection)
	total := 0
	for offset := 0; ; offset += batchSize {
		records, err := app.FindRecordsByFilter(collection, filter, "", batchSize, offset, nil)
		if err != nil {
			return total, fmt.Errorf("fetch offset %d: %w", offset, err)
		}
		if len(records) == 0 {
			break
		}

		eg, _ := errgroup.WithContext(context.Background())
		eg.SetLimit(4)
		for _, rec := range records {
			rec := rec
			eg.Go(func() error {
				doc := build(app, rec)
				return search.Upsert(doc)
			})
		}
		if err := eg.Wait(); err != nil {
			return total, fmt.Errorf("upsert batch at offset %d: %w", offset, err)
		}

		total += len(records)
		if total%batchSize == 0 || len(records) < batchSize {
			_, _ = fmt.Fprintf(os.Stdout, "[%s] indexed %d records\n", collection, total)
		}

		if len(records) < batchSize {
			break
		}
	}
	return total, nil
}
