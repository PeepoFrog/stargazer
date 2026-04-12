package selectionengine

import (
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

type ChannelSelection struct {
	Channel         string         `json:"channel"`
	RequestedFilter string         `json:"requested_filter"`
	ActualFilter    string         `json:"actual_filter"`
	FallbackRank    int            `json:"fallback_rank"`
	ProductKind     string         `json:"product_kind"`
	DataURL         string         `json:"data_url,omitempty"`
	Row             map[string]any `json:"-"`
}

type GroupCandidate struct {
	TargetName       string
	Files            map[string]map[string]any
	Channels         map[string]ChannelSelection
	AvgDist          float64
	Score            float64
	FallbackPenalty  float64
	DuplicatePenalty float64
	SelectionMode    string
	ProductKind      string
}

func MustChooseBest(
	rows []map[string]any,
	inputTarget string,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
	allowSingleFilter bool,
	debug bool,
) GroupCandidate {
	best, err := ChooseBestRGBSet(
		rows,
		inputTarget,
		cfg,
		allowCalFits,
		allowSingleFilter,
		debug,
	)
	if err != nil {
		log.Fatalf("choose best rgb set: %v", err)
	}

	log.Printf(
		"Selected target group=%q score=%.2f avg_distance=%.3f mode=%s product_kind=%s R=%s G=%s B=%s",
		best.TargetName,
		best.Score,
		best.AvgDist,
		best.SelectionMode,
		best.ProductKind,
		best.Channels["red"].ActualFilter,
		best.Channels["green"].ActualFilter,
		best.Channels["blue"].ActualFilter,
	)

	return best
}

func ChooseBestRGBSet(
	rows []map[string]any,
	inputTarget string,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
	allowSingleFilter bool,
	debug bool,
) (GroupCandidate, error) {
	wantedFilters := map[string]bool{}
	for _, spec := range cfg.RGBSpecs {
		for _, filterName := range spec.PreferredFilters {
			wantedFilters[filterName] = true
		}
	}

	groups := map[string]*GroupCandidate{}
	for _, row := range rows {
		filterName := strings.ToUpper(strings.TrimSpace(asString(row["filters"])))
		dataURL := strings.TrimSpace(asString(row["dataURL"]))
		targetName := strings.TrimSpace(asString(row["target_name"]))
		productKind := acceptedProductKind(cfg, dataURL, allowCalFits)

		if !wantedFilters[filterName] {
			if debug {
				log.Printf("Skipping row target=%q filter=%q: unsupported filter", targetName, filterName)
			}
			continue
		}

		if productKind == "" {
			if debug {
				log.Printf("Skipping row target=%q filter=%q: dataURL is not accepted (%q)", targetName, filterName, dataURL)
			}
			continue
		}

		if targetName == "" {
			if debug {
				log.Printf("Skipping row with filter=%q: empty target_name", filterName)
			}
			continue
		}

		g, ok := groups[targetName]
		if !ok {
			g = &GroupCandidate{
				TargetName: targetName,
				Files:      map[string]map[string]any{},
			}
			groups[targetName] = g
		}

		current, exists := g.Files[filterName]
		if !exists || betterRow(row, current, cfg, allowCalFits) {
			g.Files[filterName] = row
		}
	}

	var candidates []GroupCandidate
	for _, g := range groups {
		channels, fallbackPenalty, duplicatePenalty, calibSum, distSum, selectionMode, productKind, ok :=
			selectRGBChannels(g.Files, cfg, allowCalFits, allowSingleFilter)
		if !ok {
			continue
		}

		g.Channels = channels
		g.AvgDist = distSum / 3.0
		g.FallbackPenalty = fallbackPenalty
		g.DuplicatePenalty = duplicatePenalty
		g.SelectionMode = selectionMode
		g.ProductKind = productKind

		score := 3000.0 + calibSum*10.0 - g.AvgDist - fallbackPenalty - duplicatePenalty
		score -= productKindPenalty(productKind)
		if selectionMode == "single_filter_fallback" {
			score -= 600.0
		}

		if normalizedName(g.TargetName) == normalizedName(inputTarget) {
			score += 500.0
		} else if strings.Contains(normalizedName(g.TargetName), normalizedName(inputTarget)) ||
			strings.Contains(normalizedName(inputTarget), normalizedName(g.TargetName)) {
			score += 250.0
		}
		if !strings.Contains(strings.ToUpper(g.TargetName), "POSITION") &&
			!strings.Contains(strings.ToUpper(g.TargetName), "MRS") {
			score += 75.0
		}

		g.Score = score
		candidates = append(candidates, *g)
	}

	if len(candidates) == 0 {
		msg := "no usable RGB set found"
		if allowCalFits && allowSingleFilter {
			msg += " (checked _i2d.fits, _cal.fits, and single-filter fallback)"
		} else if allowCalFits {
			msg += " (checked _i2d.fits and _cal.fits)"
		} else {
			msg += " (checked _i2d.fits only)"
		}
		return GroupCandidate{}, errors.New(msg)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].FallbackPenalty == candidates[j].FallbackPenalty {
				if candidates[i].DuplicatePenalty == candidates[j].DuplicatePenalty {
					return candidates[i].AvgDist < candidates[j].AvgDist
				}
				return candidates[i].DuplicatePenalty < candidates[j].DuplicatePenalty
			}
			return candidates[i].FallbackPenalty < candidates[j].FallbackPenalty
		}
		return candidates[i].Score > candidates[j].Score
	})

	return candidates[0], nil
}

func LogTargetRowsSummary(rows []map[string]any, inputTarget string, cfg rgb_configs.SourceConfig, allowCalFits bool) {
	log.Printf("Debug summary for target=%q: raw rows=%d", inputTarget, len(rows))

	type stat struct {
		Count         int
		I2DCount      int
		CalCount      int
		AcceptedCount int
		WithURL       int
	}

	perFilter := map[string]*stat{}
	targetNames := map[string]int{}

	for _, row := range rows {
		filterName := strings.ToUpper(strings.TrimSpace(asString(row["filters"])))
		targetName := strings.TrimSpace(asString(row["target_name"]))
		dataURL := strings.TrimSpace(asString(row["dataURL"]))
		lower := strings.ToLower(dataURL)

		if targetName != "" {
			targetNames[targetName]++
		}

		s, ok := perFilter[filterName]
		if !ok {
			s = &stat{}
			perFilter[filterName] = s
		}
		s.Count++

		if dataURL != "" {
			s.WithURL++
		}
		if strings.HasSuffix(lower, "_i2d.fits") {
			s.I2DCount++
		}
		if strings.HasSuffix(lower, "_cal.fits") {
			s.CalCount++
		}
		if acceptedProductKind(cfg, dataURL, allowCalFits) != "" {
			s.AcceptedCount++
		}
	}

	var filters []string
	for f := range perFilter {
		filters = append(filters, f)
	}
	sort.Strings(filters)

	log.Printf("Debug target names found (%d unique):", len(targetNames))
	for name, n := range targetNames {
		log.Printf("  target_name=%q rows=%d", name, n)
	}

	log.Printf("Debug per-filter summary:")
	for _, f := range filters {
		s := perFilter[f]
		log.Printf(
			"  filter=%q rows=%d with_url=%d i2d=%d cal=%d accepted=%d",
			f, s.Count, s.WithURL, s.I2DCount, s.CalCount, s.AcceptedCount,
		)
	}
}

func selectRGBChannels(
	available map[string]map[string]any,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
	allowSingleFilter bool,
) (map[string]ChannelSelection, float64, float64, float64, float64, string, string, bool) {
	if len(cfg.RGBSpecs) != 3 {
		return nil, 0, 0, 0, 0, "", "", false
	}

	redChoices := buildChannelSelections(available, cfg.RGBSpecs[0], cfg, allowCalFits)
	greenChoices := buildChannelSelections(available, cfg.RGBSpecs[1], cfg, allowCalFits)
	blueChoices := buildChannelSelections(available, cfg.RGBSpecs[2], cfg, allowCalFits)

	var best map[string]ChannelSelection
	bestScore := math.Inf(-1)
	bestFallbackPenalty := 0.0
	bestDuplicatePenalty := 0.0
	bestCalibSum := 0.0
	bestDistSum := 0.0
	bestProductKind := ""

	if len(redChoices) > 0 && len(greenChoices) > 0 && len(blueChoices) > 0 {
		for _, r := range redChoices {
			for _, g := range greenChoices {
				for _, b := range blueChoices {
					if !rowsCompatibleForRGB(cfg, r.Row, g.Row, b.Row) {
						continue
					}

					if uniqueFilterCount(r.ActualFilter, g.ActualFilter, b.ActualFilter) < 2 {
						continue
					}

					fallbackPenalty := float64(r.FallbackRank+g.FallbackRank+b.FallbackRank) * 100.0
					duplicatePenalty := duplicateFilterPenalty(r.ActualFilter, g.ActualFilter, b.ActualFilter)
					productKind := mergedProductKind(r.ProductKind, g.ProductKind, b.ProductKind)
					productPenalty := productKindPenalty(productKind)

					calibSum := asFloat(r.Row["calib_level"]) + asFloat(g.Row["calib_level"]) + asFloat(b.Row["calib_level"])
					distSum := asFloat(r.Row["distance"]) + asFloat(g.Row["distance"]) + asFloat(b.Row["distance"])

					areaSum := float64(rowPixelArea(r.Row) + rowPixelArea(g.Row) + rowPixelArea(b.Row))
					areaBonus := areaSum / 1_000_000.0

					score := calibSum*10.0 - distSum - fallbackPenalty - duplicatePenalty - productPenalty + areaBonus
					if best == nil || score > bestScore {
						bestScore = score
						bestFallbackPenalty = fallbackPenalty
						bestDuplicatePenalty = duplicatePenalty
						bestCalibSum = calibSum
						bestDistSum = distSum
						bestProductKind = productKind
						best = map[string]ChannelSelection{
							"red":   r,
							"green": g,
							"blue":  b,
						}
					}
				}
			}
		}
	}

	if best != nil {
		return best, bestFallbackPenalty, bestDuplicatePenalty, bestCalibSum, bestDistSum, "rgb", bestProductKind, true
	}

	if !allowSingleFilter {
		return nil, 0, 0, 0, 0, "", "", false
	}

	actualFilter, row, ok := bestSingleFilterRow(cfg, available, allowCalFits)
	if !ok {
		return nil, 0, 0, 0, 0, "", "", false
	}

	productKind := acceptedProductKind(cfg, asString(row["dataURL"]), allowCalFits)
	rRank := indexOfPreferredFilter(cfg.RGBSpecs[0].PreferredFilters, actualFilter)
	gRank := indexOfPreferredFilter(cfg.RGBSpecs[1].PreferredFilters, actualFilter)
	bRank := indexOfPreferredFilter(cfg.RGBSpecs[2].PreferredFilters, actualFilter)

	selection := map[string]ChannelSelection{
		"red": {
			Channel:         "red",
			RequestedFilter: cfg.RGBSpecs[0].PreferredFilters[0],
			ActualFilter:    actualFilter,
			FallbackRank:    rRank,
			ProductKind:     productKind,
			DataURL:         asString(row["dataURL"]),
			Row:             row,
		},
		"green": {
			Channel:         "green",
			RequestedFilter: cfg.RGBSpecs[1].PreferredFilters[0],
			ActualFilter:    actualFilter,
			FallbackRank:    gRank,
			ProductKind:     productKind,
			DataURL:         asString(row["dataURL"]),
			Row:             row,
		},
		"blue": {
			Channel:         "blue",
			RequestedFilter: cfg.RGBSpecs[2].PreferredFilters[0],
			ActualFilter:    actualFilter,
			FallbackRank:    bRank,
			ProductKind:     productKind,
			DataURL:         asString(row["dataURL"]),
			Row:             row,
		},
	}

	fallbackPenalty := float64(rRank+gRank+bRank)*100.0 + 400.0
	duplicatePenalty := 400.0
	calibSum := asFloat(row["calib_level"]) * 3.0
	distSum := asFloat(row["distance"]) * 3.0

	return selection, fallbackPenalty, duplicatePenalty, calibSum, distSum, "single_filter_fallback", productKind, true
}

func buildChannelSelections(
	available map[string]map[string]any,
	spec rgb_configs.RgbChannelSpec,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
) []ChannelSelection {
	var out []ChannelSelection
	seen := map[string]bool{}
	requested := spec.PreferredFilters[0]

	for idx, filterName := range spec.PreferredFilters {
		if seen[filterName] {
			continue
		}
		row, ok := available[filterName]
		if !ok {
			continue
		}
		seen[filterName] = true

		out = append(out, ChannelSelection{
			Channel:         spec.Name,
			RequestedFilter: requested,
			ActualFilter:    filterName,
			FallbackRank:    idx,
			ProductKind:     acceptedProductKind(cfg, asString(row["dataURL"]), allowCalFits),
			DataURL:         asString(row["dataURL"]),
			Row:             row,
		})
	}

	return out
}

func bestSingleFilterRow(
	cfg rgb_configs.SourceConfig,
	available map[string]map[string]any,
	allowCalFits bool,
) (string, map[string]any, bool) {
	var (
		bestFilter string
		bestRow    map[string]any
	)

	for filterName, row := range available {
		if acceptedProductKind(cfg, asString(row["dataURL"]), allowCalFits) == "" {
			continue
		}
		if bestRow == nil || betterRow(row, bestRow, cfg, allowCalFits) {
			bestFilter = filterName
			bestRow = row
		}
	}

	if bestRow == nil {
		return "", nil, false
	}
	return bestFilter, bestRow, true
}

func betterRow(a, b map[string]any, cfg rgb_configs.SourceConfig, allowCalFits bool) bool {
	kindA := acceptedProductKind(cfg, asString(a["dataURL"]), allowCalFits)
	kindB := acceptedProductKind(cfg, asString(b["dataURL"]), allowCalFits)

	rankA := productKindRank(kindA)
	rankB := productKindRank(kindB)
	if rankA != rankB {
		return rankA > rankB
	}

	calA := asFloat(a["calib_level"])
	calB := asFloat(b["calib_level"])
	if calA != calB {
		return calA > calB
	}

	areaA := rowPixelArea(a)
	areaB := rowPixelArea(b)
	if areaA != areaB {
		return areaA > areaB
	}

	distA := asFloat(a["distance"])
	distB := asFloat(b["distance"])
	return distA < distB
}

func acceptedProductKind(cfg rgb_configs.SourceConfig, dataURL string, allowCalFits bool) string {
	lower := strings.ToLower(strings.TrimSpace(dataURL))

	switch strings.ToLower(strings.TrimSpace(cfg.Name)) {
	case "jwst":
		switch {
		case strings.HasSuffix(lower, "_i2d.fits"):
			return "i2d"
		case allowCalFits && strings.HasSuffix(lower, "_cal.fits"):
			return "cal"
		default:
			return ""
		}
	case "hst":
		switch {
		case strings.HasSuffix(lower, "_drc.fits"):
			return "drc"
		case strings.HasSuffix(lower, "_drz.fits"):
			return "drz"
		case allowCalFits && strings.HasSuffix(lower, "_flc.fits"):
			return "flc"
		case allowCalFits && strings.HasSuffix(lower, "_flt.fits"):
			return "flt"
		default:
			return ""
		}
	default:
		return ""
	}
}

func productKindRank(kind string) int {
	switch kind {
	case "i2d", "drc", "drz":
		return 3
	case "flc", "flt", "cal":
		return 2
	default:
		return 0
	}
}

func mergedProductKind(kinds ...string) string {
	if len(kinds) == 0 {
		return ""
	}
	first := kinds[0]
	for _, k := range kinds[1:] {
		if k != first {
			return "mixed"
		}
	}
	return first
}

func productKindPenalty(kind string) float64 {
	switch kind {
	case "i2d":
		return 0
	case "mixed":
		return 120
	case "cal":
		return 240
	default:
		return 500
	}
}

func indexOfPreferredFilter(filters []string, actual string) int {
	for i, f := range filters {
		if strings.EqualFold(strings.TrimSpace(f), strings.TrimSpace(actual)) {
			return i
		}
	}
	return len(filters) + 5
}

func uniqueFilterCount(filters ...string) int {
	seen := map[string]bool{}
	for _, f := range filters {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		seen[f] = true
	}
	return len(seen)
}

func duplicateFilterPenalty(filters ...string) float64 {
	counts := map[string]int{}
	for _, f := range filters {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		counts[f]++
	}

	penalty := 0.0
	for _, n := range counts {
		if n > 1 {
			penalty += float64(n-1) * 180.0
		}
	}
	return penalty
}

func normalizedName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeKey(s string) string {
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func firstPositiveInt(row map[string]any, keys ...string) int {
	for _, key := range keys {
		v := asFloat(row[key])
		if v > 0 {
			return int(math.Round(v))
		}
	}
	return 0
}

func rowShapeKey(row map[string]any) string {
	w := firstPositiveInt(row, "naxis1", "s_xel1", "sizex", "x_size")
	h := firstPositiveInt(row, "naxis2", "s_xel2", "sizey", "y_size")
	if w > 0 && h > 0 {
		return fmt.Sprintf("%dx%d", w, h)
	}
	return ""
}

func hstCompatibilityKey(row map[string]any) string {
	proposal := normalizeKey(asString(row["proposal_id"]))
	inst := normalizeKey(asString(row["instrument_name"]))
	det := normalizeKey(asString(row["detector"]))
	shape := rowShapeKey(row)

	parts := make([]string, 0, 4)
	if proposal != "" {
		parts = append(parts, proposal)
	}
	if inst != "" {
		parts = append(parts, inst)
	}
	if det != "" {
		parts = append(parts, det)
	}
	if shape != "" {
		parts = append(parts, shape)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "|")
}

func hstFileFamilyKey(row map[string]any) string {
	base := strings.ToLower(strings.TrimSpace(asString(row["dataURL"])))
	if base == "" {
		return ""
	}

	base = base[strings.LastIndex(base, "/")+1:]
	base = strings.TrimSuffix(base, ".fits")
	for _, suffix := range []string{"_drc", "_drz", "_flc", "_flt"} {
		base = strings.TrimSuffix(base, suffix)
	}

	parts := strings.Split(base, "_")
	if len(parts) == 0 {
		return ""
	}

	last := normalizeKey(parts[len(parts)-1])
	if strings.HasPrefix(last, "IB") || strings.HasPrefix(last, "U2") || strings.HasPrefix(last, "U5") || strings.HasPrefix(last, "J8") {
		return last
	}

	first := normalizeKey(parts[0])
	return first
}

func rowsCompatibleForRGB(cfg rgb_configs.SourceConfig, rows ...map[string]any) bool {
	if strings.ToLower(strings.TrimSpace(cfg.Name)) != "hst" {
		return true
	}

	var firstMetaKey string
	var firstFamilyKey string

	for _, row := range rows {
		metaKey := hstCompatibilityKey(row)
		familyKey := hstFileFamilyKey(row)

		if metaKey == "" && familyKey == "" {
			return false
		}

		if firstMetaKey == "" && metaKey != "" {
			firstMetaKey = metaKey
		}
		if firstFamilyKey == "" && familyKey != "" {
			firstFamilyKey = familyKey
		}

		if firstMetaKey != "" && metaKey != "" && metaKey != firstMetaKey {
			return false
		}
		if firstFamilyKey != "" && familyKey != "" && familyKey != firstFamilyKey {
			return false
		}
	}

	return true
}

func rowPixelArea(row map[string]any) int64 {
	keysW := []string{"naxis1", "s_xel1", "sizex", "x_size"}
	keysH := []string{"naxis2", "s_xel2", "sizey", "y_size"}

	var w, h int64
	for _, k := range keysW {
		v := int64(math.Round(asFloat(row[k])))
		if v > 0 {
			w = v
			break
		}
	}
	for _, k := range keysH {
		v := int64(math.Round(asFloat(row[k])))
		if v > 0 {
			h = v
			break
		}
	}
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
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

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		var out float64
		if _, err := fmt.Sscanf(t, "%f", &out); err == nil {
			return out
		}
		return 0
	default:
		return 0
	}
}
