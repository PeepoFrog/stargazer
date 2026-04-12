package searchflow

import (
	"log"
	"net/http"

	"github.com/PeepFrog/datastsciparser/internal/cli"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

type Resolver func(client *http.Client, name string, verbose bool) (string, float64, float64, error)
type PositionSearcher func(client *http.Client, cfg rgb_configs.SourceConfig, ra, dec, radiusDeg float64, verbose bool) ([]map[string]any, error)
type ExactSearcher func(client *http.Client, cfg rgb_configs.SourceConfig, targetName string, verbose bool) ([]map[string]any, error)
type DebugLogger func(rows []map[string]any, inputTarget string, cfg rgb_configs.SourceConfig, allowCalFits bool)

type Result struct {
	CanonicalName string
	Rows          []map[string]any
	SearchInput   string
	SearchMode    string
}

type Dependencies struct {
	ResolveName             Resolver
	SearchImagesByPosition  PositionSearcher
	SearchImagesByExactName ExactSearcher
	LogTargetRowsSummary    DebugLogger
}

func MustResolveAndSearch(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	opts cli.Options,
	deps Dependencies,
) Result {
	searchInput := opts.SearchInput()
	searchMode := opts.SearchMode()

	var (
		canonicalName string
		rows          []map[string]any
		err           error
	)

	if opts.TargetNameExact != "" {
		canonicalName = searchInput
		log.Printf("Using exact MAST target_name search for %q", searchInput)

		rows, err = deps.SearchImagesByExactName(client, cfg, searchInput, opts.Verbose)
		if err != nil {
			log.Fatalf("search observations by exact target_name: %v", err)
		}
		if len(rows) == 0 {
			log.Fatalf("no image observations found for exact target_name %q", searchInput)
		}
	} else {
		var ra, dec float64
		canonicalName, ra, dec, err = deps.ResolveName(client, searchInput, opts.Verbose)
		if err != nil {
			log.Fatalf("resolve target: %v", err)
		}

		log.Printf("Resolved %q -> canonical=%q RA=%.6f Dec=%.6f", searchInput, canonicalName, ra, dec)

		rows, err = deps.SearchImagesByPosition(client, cfg, ra, dec, opts.RadiusDeg, opts.Verbose)
		if err != nil {
			log.Fatalf("search observations: %v", err)
		}
		if len(rows) == 0 {
			log.Fatalf("no image observations found near %q", searchInput)
		}
	}

	if opts.DebugSelection && deps.LogTargetRowsSummary != nil {
		deps.LogTargetRowsSummary(rows, searchInput, cfg, opts.AllowCalFits)
	}

	return Result{
		CanonicalName: canonicalName,
		Rows:          rows,
		SearchInput:   searchInput,
		SearchMode:    searchMode,
	}
}
