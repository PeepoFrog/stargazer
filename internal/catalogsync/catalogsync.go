package catalogsync

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/PeepFrog/datastsciparser/internal/catalogdb"
	"github.com/PeepFrog/datastsciparser/internal/mastapi"
	"github.com/PeepFrog/datastsciparser/internal/selectionengine"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

type Options struct {
	PageSize          int
	TargetBatchSize   int
	AllowCalFits      bool
	AllowSingleFilter bool
	DebugSelection    bool
}

type Result struct {
	DBPath            string
	Source            string
	PagesFetched      int
	RowsFetched       int
	RowsStored        int
	TargetsTotal      int
	TargetsRenderable int
	TargetsSkipped    int
	StartedAt         time.Time
	CompletedAt       time.Time
}

func SyncFresh(
	dbPath string,
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	opts Options,
) (Result, error) {
	if client == nil {
		return Result{}, errors.New("catalogsync: nil http client")
	}

	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 300
	}

	targetBatchSize := opts.TargetBatchSize
	if targetBatchSize <= 0 {
		targetBatchSize = 250
	}

	store, err := catalogdb.Open(dbPath)
	if err != nil {
		return Result{}, fmt.Errorf("open catalog db: %w", err)
	}
	defer store.Close()

	startedAt := time.Now().UTC()

	if err := store.SetMeta("sync.started_at", startedAt.Format(time.RFC3339)); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.completed_at", ""); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.source", cfg.Name); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.page_size", fmt.Sprintf("%d", pageSize)); err != nil {
		return Result{}, err
	}

	var (
		pagesFetched int
		rowsFetched  int
		rowsStored   int
	)

	for page := 1; ; page++ {
		rows, err := mastapi.SearchImagesCatalog(
			client,
			cfg,
			page,
			pageSize,
			opts.DebugSelection,
		)
		if err != nil {
			return Result{}, fmt.Errorf("fetch catalog page=%d: %w", page, err)
		}
		if len(rows) == 0 {
			break
		}

		pagesFetched++
		rowsFetched += len(rows)

		records := make([]catalogdb.ProductRecord, 0, len(rows))
		for _, row := range rows {
			rec, err := catalogdb.NewProductRecord(cfg.Name, row)
			if errors.Is(err, catalogdb.ErrSkipProductRow) {
				continue
			}
			if err != nil {
				return Result{}, fmt.Errorf("build product record page=%d: %w", page, err)
			}
			records = append(records, rec)
		}

		if err := store.UpsertProducts(records); err != nil {
			return Result{}, fmt.Errorf("store products page=%d: %w", page, err)
		}
		rowsStored += len(records)

		log.Printf(
			"sync fetch page=%d rows_fetched=%d rows_stored=%d total_fetched=%d total_stored=%d",
			page,
			len(rows),
			len(records),
			rowsFetched,
			rowsStored,
		)

		if len(rows) < pageSize {
			break
		}
	}

	if err := store.ClearCandidates(cfg.Name); err != nil {
		return Result{}, err
	}

	targetsTotal, err := store.DistinctTargetCount(cfg.Name)
	if err != nil {
		return Result{}, err
	}

	var (
		targetsRenderable int
		targetsSkipped    int
		processedTargets  int
	)

	for offset := 0; ; {
		targetNames, err := store.ListTargetNames(cfg.Name, targetBatchSize, offset)
		if err != nil {
			return Result{}, err
		}
		if len(targetNames) == 0 {
			break
		}

		for _, targetName := range targetNames {
			rawRows, err := store.LoadRawRowsForTarget(cfg.Name, targetName)
			if err != nil {
				return Result{}, err
			}

			best, err := selectionengine.ChooseBest(
				rawRows,
				targetName,
				cfg,
				opts.AllowCalFits,
				opts.AllowSingleFilter,
				opts.DebugSelection,
			)
			if err != nil {
				targetsSkipped++
				processedTargets++
				continue
			}

			record := buildCandidateRecord(cfg.Name, targetName, rawRows, best)
			if err := store.UpsertCandidate(record); err != nil {
				return Result{}, err
			}

			targetsRenderable++
			processedTargets++
		}

		log.Printf(
			"sync rebuild candidates processed=%d/%d renderable=%d skipped=%d",
			processedTargets,
			targetsTotal,
			targetsRenderable,
			targetsSkipped,
		)

		offset += len(targetNames)
	}

	completedAt := time.Now().UTC()
	if err := store.SetMeta("sync.completed_at", completedAt.Format(time.RFC3339)); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.targets_total", fmt.Sprintf("%d", targetsTotal)); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.targets_renderable", fmt.Sprintf("%d", targetsRenderable)); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.targets_skipped", fmt.Sprintf("%d", targetsSkipped)); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.rows_fetched", fmt.Sprintf("%d", rowsFetched)); err != nil {
		return Result{}, err
	}
	if err := store.SetMeta("sync.rows_stored", fmt.Sprintf("%d", rowsStored)); err != nil {
		return Result{}, err
	}

	return Result{
		DBPath:            dbPath,
		Source:            cfg.Name,
		PagesFetched:      pagesFetched,
		RowsFetched:       rowsFetched,
		RowsStored:        rowsStored,
		TargetsTotal:      targetsTotal,
		TargetsRenderable: targetsRenderable,
		TargetsSkipped:    targetsSkipped,
		StartedAt:         startedAt,
		CompletedAt:       completedAt,
	}, nil
}

func buildCandidateRecord(
	source string,
	targetName string,
	rawRows []map[string]any,
	best selectionengine.GroupCandidate,
) catalogdb.CandidateRecord {
	now := time.Now().UTC().Format(time.RFC3339)

	return catalogdb.CandidateRecord{
		Source:           strings.ToLower(strings.TrimSpace(source)),
		TargetName:       targetName,
		TargetNameNorm:   normalizeNameForSearch(targetName),
		RowsCount:        len(rawRows),
		FiltersCSV:       strings.Join(collectDistinctFilters(rawRows), ","),
		Quality:          qualityLabel(best),
		SelectionMode:    best.SelectionMode,
		ProductKind:      best.ProductKind,
		Score:            best.Score,
		AvgDist:          best.AvgDist,
		FallbackPenalty:  best.FallbackPenalty,
		DuplicatePenalty: best.DuplicatePenalty,
		RedFilter:        channelFilter(best, "red"),
		GreenFilter:      channelFilter(best, "green"),
		BlueFilter:       channelFilter(best, "blue"),
		RedDataURL:       channelDataURL(best, "red"),
		GreenDataURL:     channelDataURL(best, "green"),
		BlueDataURL:      channelDataURL(best, "blue"),
		UpdatedAt:        now,
	}
}

func qualityLabel(c selectionengine.GroupCandidate) string {
	if c.SelectionMode == "single_filter_fallback" {
		return "single_filter"
	}

	r := channelFilter(c, "red")
	g := channelFilter(c, "green")
	b := channelFilter(c, "blue")

	uniq := map[string]struct{}{}
	for _, f := range []string{r, g, b} {
		f = strings.TrimSpace(f)
		if f == "" || f == "-" {
			continue
		}
		uniq[f] = struct{}{}
	}

	switch len(uniq) {
	case 3:
		return "strong_rgb"
	case 2:
		return "weak_rgb"
	case 1:
		return "single_filter"
	default:
		return "unknown"
	}
}

func channelFilter(c selectionengine.GroupCandidate, key string) string {
	ch, ok := c.Channels[key]
	if !ok {
		return "-"
	}
	v := strings.TrimSpace(ch.ActualFilter)
	if v == "" {
		return "-"
	}
	return v
}

func channelDataURL(c selectionengine.GroupCandidate, key string) string {
	ch, ok := c.Channels[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(ch.DataURL)
}

func collectDistinctFilters(rows []map[string]any) []string {
	seen := make(map[string]struct{}, 8)

	for _, row := range rows {
		for _, key := range []string{"filters", "filter", "filter_name"} {
			v := strings.TrimSpace(asString(row[key]))
			if v == "" {
				continue
			}
			seen[v] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func normalizeNameForSearch(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
