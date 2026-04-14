package searchflow

import (
	"fmt"
	"log"
	"net/http"
	"strings"

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
	Summary       Summary
}

type Summary struct {
	RequestedMode         string   `json:"requested_mode"`
	ExecutedPaths         []string `json:"executed_paths"`
	ExactInputRows        int      `json:"exact_input_rows"`
	ExactCanonicalRows    int      `json:"exact_canonical_rows"`
	ExactTotalRows        int      `json:"exact_total_rows"`
	PositionRows          int      `json:"position_rows"`
	MergedUniqueRows      int      `json:"merged_unique_rows"`
	ResolverSucceeded     bool     `json:"resolver_succeeded"`
	ResolverError         string   `json:"resolver_error,omitempty"`
	PositionSearchSkipped bool     `json:"position_search_skipped"`
}

type Dependencies struct {
	ResolveName             Resolver
	SearchImagesByPosition  PositionSearcher
	SearchImagesByExactName ExactSearcher
	LogTargetRowsSummary    DebugLogger
}

func MustSearch(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	opts cli.Options,
	deps Dependencies,
) Result {
	searchInput := opts.SearchInput()
	searchMode := opts.SearchModeLabel()

	result, err := search(client, cfg, opts, deps)
	if err != nil {
		log.Fatal(err)
	}

	result.SearchInput = searchInput
	result.SearchMode = searchMode

	if opts.DebugSelection && deps.LogTargetRowsSummary != nil {
		deps.LogTargetRowsSummary(result.Rows, searchInput, cfg, opts.AllowCalFits)
	}

	log.Printf(
		"Search summary: mode=%s exact_input=%d exact_canonical=%d exact_total=%d position=%d merged_unique=%d",
		result.SearchMode,
		result.Summary.ExactInputRows,
		result.Summary.ExactCanonicalRows,
		result.Summary.ExactTotalRows,
		result.Summary.PositionRows,
		result.Summary.MergedUniqueRows,
	)

	return result
}

func search(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	opts cli.Options,
	deps Dependencies,
) (Result, error) {
	searchInput := opts.SearchInput()
	summary := Summary{RequestedMode: opts.SearchMode}

	switch opts.SearchMode {
	case "exact":
		summary.ExecutedPaths = append(summary.ExecutedPaths, "exact_input")

		rows, err := deps.SearchImagesByExactName(client, cfg, searchInput, opts.Verbose)
		if err != nil {
			return Result{}, fmt.Errorf("search observations by exact target_name: %w", err)
		}

		summary.ExactInputRows = len(rows)
		summary.ExactTotalRows = len(rows)
		summary.MergedUniqueRows = len(rows)

		if len(rows) == 0 {
			return Result{}, fmt.Errorf("no image observations found for exact target_name %q", searchInput)
		}

		return Result{
			CanonicalName: searchInput,
			Rows:          rows,
			Summary:       summary,
		}, nil

	case "position":
		summary.ExecutedPaths = append(summary.ExecutedPaths, "resolve_name", "position")

		canonicalName, ra, dec, err := deps.ResolveName(client, searchInput, opts.Verbose)
		if err != nil {
			return Result{}, fmt.Errorf("resolve target: %w", err)
		}
		summary.ResolverSucceeded = true

		log.Printf("Resolved %q -> canonical=%q RA=%.6f Dec=%.6f", searchInput, canonicalName, ra, dec)

		rows, err := deps.SearchImagesByPosition(client, cfg, ra, dec, opts.RadiusDeg, opts.Verbose)
		if err != nil {
			return Result{}, fmt.Errorf("search observations by position: %w", err)
		}

		summary.PositionRows = len(rows)
		summary.MergedUniqueRows = len(rows)

		if len(rows) == 0 {
			return Result{}, fmt.Errorf("no image observations found near %q", searchInput)
		}

		return Result{
			CanonicalName: canonicalName,
			Rows:          rows,
			Summary:       summary,
		}, nil

	case "auto":
		return autoSearch(client, cfg, opts, deps, summary)

	default:
		return Result{}, fmt.Errorf("unsupported search mode %q", opts.SearchMode)
	}
}

func autoSearch(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	opts cli.Options,
	deps Dependencies,
	summary Summary,
) (Result, error) {
	searchInput := opts.SearchInput()
	summary.ExecutedPaths = append(summary.ExecutedPaths, "exact_input")

	exactInputRows, err := deps.SearchImagesByExactName(client, cfg, searchInput, opts.Verbose)
	if err != nil {
		return Result{}, fmt.Errorf("search observations by exact target_name: %w", err)
	}
	summary.ExactInputRows = len(exactInputRows)

	canonicalName := searchInput
	var exactCanonicalRows []map[string]any
	var positionRows []map[string]any

	resolvedCanonical, ra, dec, resolveErr := deps.ResolveName(client, searchInput, opts.Verbose)
	if resolveErr != nil {
		summary.ResolverError = resolveErr.Error()
		summary.PositionSearchSkipped = true

		if len(exactInputRows) == 0 {
			return Result{}, fmt.Errorf("exact search found 0 rows and resolve target failed: %w", resolveErr)
		}

		log.Printf("Auto search: resolve failed for %q, using exact results only: %v", searchInput, resolveErr)
	} else {
		summary.ResolverSucceeded = true
		canonicalName = resolvedCanonical

		log.Printf("Resolved %q -> canonical=%q RA=%.6f Dec=%.6f", searchInput, canonicalName, ra, dec)

		if normalized(searchInput) != normalized(canonicalName) {
			summary.ExecutedPaths = append(summary.ExecutedPaths, "exact_canonical")

			exactCanonicalRows, err = deps.SearchImagesByExactName(client, cfg, canonicalName, opts.Verbose)
			if err != nil {
				return Result{}, fmt.Errorf("search observations by canonical exact target_name: %w", err)
			}
			summary.ExactCanonicalRows = len(exactCanonicalRows)
		}

		summary.ExecutedPaths = append(summary.ExecutedPaths, "position")

		positionRows, err = deps.SearchImagesByPosition(client, cfg, ra, dec, opts.RadiusDeg, opts.Verbose)
		if err != nil {
			return Result{}, fmt.Errorf("search observations by position: %w", err)
		}
		summary.PositionRows = len(positionRows)
	}

	summary.ExactTotalRows = summary.ExactInputRows + summary.ExactCanonicalRows
	merged := mergeRows(exactInputRows, exactCanonicalRows, positionRows)
	summary.MergedUniqueRows = len(merged)

	if len(merged) == 0 {
		return Result{}, fmt.Errorf(
			"no image observations found for %q (exact_input=%d exact_canonical=%d position=%d)",
			searchInput,
			summary.ExactInputRows,
			summary.ExactCanonicalRows,
			summary.PositionRows,
		)
	}

	return Result{
		CanonicalName: canonicalName,
		Rows:          merged,
		Summary:       summary,
	}, nil
}

func mergeRows(parts ...[]map[string]any) []map[string]any {
	seen := map[string]bool{}
	merged := make([]map[string]any, 0)

	for _, rows := range parts {
		for _, row := range rows {
			key := dedupeKey(row)
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, row)
		}
	}

	return merged
}

func dedupeKey(row map[string]any) string {
	if dataURL := strings.TrimSpace(asString(row["dataURL"])); dataURL != "" {
		return "dataurl:" + dataURL
	}

	parts := []string{
		strings.TrimSpace(asString(row["obsid"])),
		strings.TrimSpace(asString(row["obs_id"])),
		strings.TrimSpace(asString(row["obs_collection"])),
		strings.TrimSpace(asString(row["target_name"])),
		strings.TrimSpace(asString(row["filters"])),
	}
	return strings.Join(parts, "|")
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func normalized(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}
