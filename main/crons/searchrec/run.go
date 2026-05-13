package searchrec

import (
	"context"
	"fmt"
	"mensadb/tools/search"

	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/sync/errgroup"
)

// Run performs search index reconciliation by comparing document counts
// between PocketBase collections and the Bleve search index.
func Run(app core.App) {
	// Type mappings: collection name -> singular type for search index
	types := map[string]string{
		"events":     "event",
		"sigs":       "sig",
		"deals":      "deal",
		"documents":  "document",
		"users":      "user",
	}

	// Use errgroup to fan out count operations in parallel
	eg, _ := errgroup.WithContext(context.Background())

	// Store results to avoid concurrent map writes
	results := make(map[string]struct {
		pbCount    int64
		bleveCount uint64
	})

	for collection, singularType := range types {
		// Capture loop variables to avoid closure issues
		col := collection
		typ := singularType

		eg.Go(func() error {
			// Count documents in PocketBase
			pbCount, err := app.CountRecords(col, nil)
			if err != nil {
				app.Logger().Error(fmt.Sprintf("[CRON] Search index reconciliation failed for %s: PB count error", col), "err", err)
				return nil // Continue with other types
			}

			// Count documents in Bleve index
			bleveCount, err := search.CountByType(typ)
			if err != nil {
				app.Logger().Error(fmt.Sprintf("[CRON] Search index reconciliation failed for %s: Bleve count error", col), "err", err)
				return nil // Continue with other types
			}

			// Store result (safe because errgroup ensures sequential access)
			results[col] = struct {
				pbCount    int64
				bleveCount uint64
			}{pbCount, bleveCount}

			return nil
		})
	}

	// Wait for all goroutines to complete
	_ = eg.Wait()

	// Compare and log results
	for collection, singularType := range types {
		if result, ok := results[collection]; ok {
			pbCount := result.pbCount
			bleveCount := int64(result.bleveCount)

			// Calculate discrepancy ratio
			maxCount := pbCount
			if bleveCount > pbCount {
				maxCount = bleveCount
			}
			if maxCount == 0 {
				maxCount = 1 // Avoid division by zero
			}

			discrepancy := float64(intAbs(pbCount-bleveCount)) / float64(maxCount)

			if discrepancy > 0.02 {
				app.Logger().Warn(
					fmt.Sprintf("[CRON] Search index mismatch for %s (%s)", collection, singularType),
					"pb_count", pbCount,
					"bleve_count", bleveCount,
					"discrepancy_ratio", fmt.Sprintf("%.2f%%", discrepancy*100),
				)
			} else {
				app.Logger().Info(
					fmt.Sprintf("[CRON] Search index reconciliation OK for %s (%s)", collection, singularType),
					"pb_count", pbCount,
					"bleve_count", bleveCount,
				)
			}
		}
	}
}

// intAbs returns the absolute value of an integer.
func intAbs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
