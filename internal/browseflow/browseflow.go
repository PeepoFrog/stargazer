package browseflow

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/PeepFrog/datastsciparser/internal/selectionengine"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

type BrowseOptions struct {
	Limit             int
	Offset            int
	AllowCalFits      bool
	AllowSingleFilter bool
	DebugSelection    bool
}

type BrowseItem struct {
	TargetName string
	RowsCount  int
	Filters    []string

	Candidate selectionengine.GroupCandidate
	RawRows   []map[string]any
}

type LoadResult struct {
	Items            []BrowseItem
	TotalRows        int
	TotalGroups      int
	RenderableGroups int
	SkippedGroups    int
}

type Dependencies struct {
	FetchRows func(
		client *http.Client,
		cfg rgb_configs.SourceConfig,
		opts BrowseOptions,
	) ([]map[string]any, error)

	ChooseBest func(
		rows []map[string]any,
		searchInput string,
		cfg rgb_configs.SourceConfig,
		allowCalFits bool,
		allowSingleFilter bool,
		debugSelection bool,
	) (selectionengine.GroupCandidate, error)
}

func LoadItems(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	opts BrowseOptions,
	deps Dependencies,
) (LoadResult, error) {
	if deps.FetchRows == nil {
		return LoadResult{}, errors.New("browseflow: FetchRows dependency is nil")
	}
	if deps.ChooseBest == nil {
		return LoadResult{}, errors.New("browseflow: ChooseBest dependency is nil")
	}

	rows, err := deps.FetchRows(client, cfg, opts)
	if err != nil {
		return LoadResult{}, err
	}

	grouped := groupRowsByTargetName(rows)
	groupNames := sortedKeys(grouped)

	items := make([]BrowseItem, 0, len(groupNames))
	skipped := 0

	for _, groupName := range groupNames {
		groupRows := grouped[groupName]

		candidate, err := deps.ChooseBest(
			groupRows,
			groupName,
			cfg,
			opts.AllowCalFits,
			opts.AllowSingleFilter,
			opts.DebugSelection,
		)
		if err != nil {
			skipped++
			continue
		}

		items = append(items, BrowseItem{
			TargetName: groupName,
			RowsCount:  len(groupRows),
			Filters:    collectDistinctFilters(groupRows),
			Candidate:  candidate,
			RawRows:    groupRows,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		ai := items[i].Candidate
		aj := items[j].Candidate

		if ai.Score != aj.Score {
			return ai.Score > aj.Score
		}
		if ai.SelectionMode != aj.SelectionMode {
			return ai.SelectionMode == "rgb"
		}
		if items[i].RowsCount != items[j].RowsCount {
			return items[i].RowsCount > items[j].RowsCount
		}
		return items[i].TargetName < items[j].TargetName
	})

	return LoadResult{
		Items:            items,
		TotalRows:        len(rows),
		TotalGroups:      len(grouped),
		RenderableGroups: len(items),
		SkippedGroups:    skipped,
	}, nil
}

func groupRowsByTargetName(rows []map[string]any) map[string][]map[string]any {
	out := make(map[string][]map[string]any, len(rows))

	for _, row := range rows {
		name := normalizeTargetName(extractTargetName(row))
		if name == "" {
			name = "UNKNOWN_TARGET"
		}
		out[name] = append(out[name], row)
	}

	return out
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

func extractTargetName(row map[string]any) string {
	for _, key := range []string{
		"target_name",
		"target",
		"targetid",
		"target_id",
		"obs_target_name",
	} {
		v := strings.TrimSpace(asString(row[key]))
		if v != "" {
			return v
		}
	}

	return ""
}

func normalizeTargetName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	s = strings.Join(strings.Fields(s), " ")
	return s
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})
	return keys
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return ""
	}
}
