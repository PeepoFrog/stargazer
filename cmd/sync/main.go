package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/PeepFrog/datastsciparser/internal/catalogdb"
	"github.com/PeepFrog/datastsciparser/internal/catalogsync"
	"github.com/PeepFrog/datastsciparser/internal/userdatapath"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

func main() {
	var (
		source            = flag.String("source", "jwst", "data source: jwst|hst")
		force             = flag.Bool("force", false, "delete existing synced DB and rebuild from scratch")
		pageSize          = flag.Int("page-size", 300, "page size for MAST catalog sync")
		targetBatchSize   = flag.Int("target-batch-size", 250, "how many distinct targets to rebuild per DB batch")
		allowCalFits      = flag.Bool("allow-cal-fits", false, "allow calibrated FITS fallback products")
		allowSingleFilter = flag.Bool("allow-single-filter", false, "allow single-filter fallback candidates")
		debugSelection    = flag.Bool("debug-selection", false, "enable verbose selection logs")
	)
	flag.Parse()

	cfg, err := rgb_configs.GetSourceConfig(*source)
	if err != nil {
		log.Fatalf("source config: %v", err)
	}

	dbPath, err := userdatapath.CatalogDBPath(cfg.Name)
	if err != nil {
		log.Fatalf("resolve catalog db path: %v", err)
	}

	if *force {
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			log.Fatalf("remove existing db for -force: %v", err)
		}
		log.Printf("Removed existing DB because -force is set: %s", dbPath)
	} else {
		store, err := catalogdb.Open(dbPath)
		if err != nil {
			log.Fatalf("open existing db check: %v", err)
		}

		alreadySynced, completedAt, err := store.HasCompletedSync()
		_ = store.Close()
		if err != nil {
			log.Fatalf("check existing sync status: %v", err)
		}

		if alreadySynced {
			log.Printf("Catalog DB already exists and has a completed sync: %s", dbPath)
			log.Printf("Completed at: %s", completedAt)
			log.Printf("Use -force to rebuild from scratch.")
			return
		}
	}

	client := &http.Client{Timeout: 180 * time.Second}

	result, err := catalogsync.SyncFresh(
		dbPath,
		client,
		cfg,
		catalogsync.Options{
			PageSize:          *pageSize,
			TargetBatchSize:   *targetBatchSize,
			AllowCalFits:      *allowCalFits,
			AllowSingleFilter: *allowSingleFilter,
			DebugSelection:    *debugSelection,
		},
	)
	if err != nil {
		log.Fatalf("catalog sync: %v", err)
	}

	log.Printf(
		"sync completed source=%s db=%s pages=%d rows_fetched=%d rows_stored=%d targets_total=%d renderable=%d skipped=%d started_at=%s completed_at=%s",
		result.Source,
		result.DBPath,
		result.PagesFetched,
		result.RowsFetched,
		result.RowsStored,
		result.TargetsTotal,
		result.TargetsRenderable,
		result.TargetsSkipped,
		result.StartedAt.Format(time.RFC3339),
		result.CompletedAt.Format(time.RFC3339),
	)
}
