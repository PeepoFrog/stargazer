package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PeepFrog/datastsciparser/internal/browseflow"
	"github.com/PeepFrog/datastsciparser/internal/selectionengine"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

func main() {
	var (
		source            = flag.String("source", "jwst", "data source: jwst|hst")
		limit             = flag.Int("limit", 300, "page size for catalog browse")
		offset            = flag.Int("offset", 0, "page-aligned offset")
		allowCalFits      = flag.Bool("allow-cal-fits", false, "allow fallback calibrated FITS products")
		allowSingleFilter = flag.Bool("allow-single-filter", false, "allow single-filter RGB fallback")
		debugSelection    = flag.Bool("debug-selection", false, "enable verbose selection logs")
		maxPrint          = flag.Int("max-print", 50, "max number of browse items to print")
	)
	flag.Parse()

	cfg, err := rgb_configs.GetSourceConfig(*source)
	if err != nil {
		log.Fatalf("source config: %v", err)
	}

	client := &http.Client{Timeout: 180 * time.Second}

	result, err := browseflow.LoadItems(
		client,
		cfg,
		browseflow.BrowseOptions{
			Limit:             *limit,
			Offset:            *offset,
			AllowCalFits:      *allowCalFits,
			AllowSingleFilter: *allowSingleFilter,
			DebugSelection:    *debugSelection,
		},
		browseflow.Dependencies{
			FetchRows:  browseflow.FetchRowsFromMAST,
			ChooseBest: selectionengine.ChooseBest,
		},
	)
	if err != nil {
		log.Fatalf("browse load: %v", err)
	}

	if len(result.Items) == 0 {
		fmt.Printf(
			"summary: total_rows=%d total_groups=%d renderable=%d skipped=%d\n",
			result.TotalRows,
			result.TotalGroups,
			result.RenderableGroups,
			result.SkippedGroups,
		)
		os.Exit(0)
	}

	toPrint := len(result.Items)
	if *maxPrint > 0 && toPrint > *maxPrint {
		toPrint = *maxPrint
	}

	for i := 0; i < toPrint; i++ {
		item := result.Items[i]
		best := item.Candidate

		fmt.Printf(
			"[%03d] target=%q rows=%d filters=%s quality=%s mode=%s product=%s score=%.2f rgb=%s/%s/%s\n",
			i+1,
			item.TargetName,
			item.RowsCount,
			strings.Join(item.Filters, ","),
			qualityLabel(best),
			best.SelectionMode,
			best.ProductKind,
			best.Score,
			channelFilter(best, "red"),
			channelFilter(best, "green"),
			channelFilter(best, "blue"),
		)
	}

	fmt.Printf(
		"summary: total_rows=%d total_groups=%d renderable=%d skipped=%d printed=%d\n",
		result.TotalRows,
		result.TotalGroups,
		result.RenderableGroups,
		result.SkippedGroups,
		toPrint,
	)
}

func channelFilter(c selectionengine.GroupCandidate, key string) string {
	ch, ok := c.Channels[key]
	if !ok {
		return "-"
	}
	if strings.TrimSpace(ch.ActualFilter) == "" {
		return "-"
	}
	return ch.ActualFilter
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
		if strings.TrimSpace(f) == "" || f == "-" {
			continue
		}
		uniq[f] = struct{}{}
	}

	if len(uniq) >= 3 {
		return "strong_rgb"
	}
	if len(uniq) == 2 {
		return "weak_rgb"
	}
	return "unknown"
}
