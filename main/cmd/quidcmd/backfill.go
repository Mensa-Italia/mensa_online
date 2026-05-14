package quidcmd

import (
	"fmt"
	"os"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"

	"mensadb/tools/quidsync"
)

// New espone il comando `quid-backfill`: scarica tutti gli articoli di tutti
// i numeri Quid noti e li upserta nella collection `quid_articles`. L'hook
// indexQuidArticleAsync li propaga automaticamente all'indice Bleve.
//
// Da usare al primo deploy per popolare lo storico e in disaster recovery.
// Il cron quotidiano quidnotify sincronizza solo il numero corrente.
func New(app core.App) *cobra.Command {
	return &cobra.Command{
		Use:   "quid-backfill",
		Short: "Sync di tutti gli articoli Quid (numeri WordPress) nella cache locale",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			perIssue, err := quidsync.SyncAllIssues(app)
			if err != nil {
				return fmt.Errorf("backfill: %w", err)
			}

			total := 0
			for _, n := range perIssue {
				total += n
			}
			_, _ = fmt.Fprintf(os.Stdout, "\nquid backfill completato in %s\n", time.Since(start).Round(time.Millisecond))
			_, _ = fmt.Fprintf(os.Stdout, "  %d numeri, %d articoli totali\n", len(perIssue), total)
			for n, c := range perIssue {
				_, _ = fmt.Fprintf(os.Stdout, "  Quid %2d: %d articoli\n", n, c)
			}
			return nil
		},
	}
}
