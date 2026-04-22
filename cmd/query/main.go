package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/PeepFrog/datastsciparser/internal/catalogdb"
	"github.com/PeepFrog/datastsciparser/internal/userdatapath"
)

func main() {
	var (
		source  = flag.String("source", "jwst", "data source: jwst|hst")
		query   = flag.String("q", "", "search target name")
		quality = flag.String("quality", "", "quality filter: strong_rgb|weak_rgb|single_filter")
		limit   = flag.Int("limit", 25, "max number of results")
		offset  = flag.Int("offset", 0, "result offset")
	)
	flag.Parse()

	dbPath, err := userdatapath.CatalogDBPath(*source)
	if err != nil {
		log.Fatalf("resolve db path: %v", err)
	}

	store, err := catalogdb.Open(dbPath)
	if err != nil {
		log.Fatalf("open catalog db: %v", err)
	}
	defer store.Close()

	opts := catalogdb.ListCandidatesOptions{
		Source:  *source,
		Query:   *query,
		Quality: *quality,
		Limit:   *limit,
		Offset:  *offset,
	}

	total, err := store.CountCandidatesFiltered(opts)
	if err != nil {
		log.Fatalf("count candidates: %v", err)
	}

	items, err := store.ListCandidates(opts)
	if err != nil {
		log.Fatalf("list candidates: %v", err)
	}

	for i, item := range items {
		fmt.Printf(
			"[%03d] target=%q obs=%q rows=%d quality=%s mode=%s product=%s score=%.2f rgb=%s/%s/%s filters=%s\n",
			*offset+i+1,
			item.TargetName,
			displayValue(item.ObservationID),
			item.RowsCount,
			item.Quality,
			item.SelectionMode,
			item.ProductKind,
			item.Score,
			displayFilter(item.RedFilter),
			displayFilter(item.GreenFilter),
			displayFilter(item.BlueFilter),
			item.FiltersCSV,
		)
	}

	fmt.Printf(
		"summary: source=%s query=%q quality=%q total=%d returned=%d offset=%d limit=%d\n",
		strings.ToLower(strings.TrimSpace(*source)),
		*query,
		*quality,
		total,
		len(items),
		*offset,
		*limit,
	)
}

func displayFilter(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}

func displayValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}
