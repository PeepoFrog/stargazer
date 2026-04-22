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
	ObservationKey   string
	ObservationID    string
	Files            map[string]map[string]any
	Channels         map[string]ChannelSelection
	AvgDist          float64
	Score            float64
	FallbackPenalty  float64
	DuplicatePenalty float64
	SelectionMode    string
	ProductKind      string
}

type observationGroup struct {
	TargetName     string
	ObservationKey string
	ObservationID  string
	Files          map[string]map[string]any
}

func MustChooseBest(
	rows []map[string]any,
	inputTarget string,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
	allowSingleFilter bool,
	debug bool,
) GroupCandidate {
	best, err := ChooseBest(
		rows,
		inputTarget,
		cfg,
		allowCalFits,
		allowSingleFilter,
		debug,
	)
	if err != nil {
		panic(fmt.Errorf("choose best rgb set: %w", err))
	}

	log.Printf(
		"Selected target=%q observation=%q score=%.2f avg_distance=%.3f mode=%s product_kind=%s R=%s G=%s B=%s",
		best.TargetName,
		best.ObservationID,
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

func ChooseBest(
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
			token := normalizeFilterToken(filterName)
			if token != "" {
				wantedFilters[token] = true
			}
		}
	}

	targetGroups := map[string]map[string]*observationGroup{}

	for _, row := range rows {
		dataURL := strings.TrimSpace(asString(row["dataURL"]))
		targetName := strings.TrimSpace(asString(row["target_name"]))
		productKind := acceptedProductKind(cfg, dataURL, allowCalFits)

		if productKind == "" {
			if debug {
				log.Printf("Skipping row target=%q filters=%q: dataURL is not accepted (%q)", targetName, asString(row["filters"]), dataURL)
			}
			continue
		}

		if targetName == "" {
			if debug {
				log.Printf("Skipping row with filters=%q: empty target_name", asString(row["filters"]))
			}
			continue
		}

		observationKey := compatibilityGroupKey(cfg, row)
		if observationKey == "" {
			if debug {
				log.Printf("Skipping row target=%q filters=%q: no observation compatibility key", targetName, asString(row["filters"]))
			}
			continue
		}

		matchedFilters := matchedRequestedFilters(row, wantedFilters)
		if len(matchedFilters) == 0 {
			if debug {
				log.Printf("Skipping row target=%q filters=%q: no requested filter match", targetName, asString(row["filters"]))
			}
			continue
		}

		observationID := observationDisplayID(cfg, row)

		perTarget, ok := targetGroups[targetName]
		if !ok {
			perTarget = map[string]*observationGroup{}
			targetGroups[targetName] = perTarget
		}

		grp, ok := perTarget[observationKey]
		if !ok {
			grp = &observationGroup{
				TargetName:     targetName,
				ObservationKey: observationKey,
				ObservationID:  observationID,
				Files:          map[string]map[string]any{},
			}
			perTarget[observationKey] = grp
		}

		for _, matchedFilter := range matchedFilters {
			current, exists := grp.Files[matchedFilter]
			if !exists || betterRowForMatchedFilter(row, current, matchedFilter, cfg, allowCalFits) {
				grp.Files[matchedFilter] = row
			}
		}
	}

	var candidates []GroupCandidate

	for _, perTarget := range targetGroups {
		for _, grp := range perTarget {
			channels, fallbackPenalty, duplicatePenalty, calibSum, distSum, selectionMode, productKind, ok :=
				selectRGBChannels(grp.Files, cfg, allowCalFits, allowSingleFilter)
			if !ok {
				continue
			}

			avgDist := distSum / 3.0
			score := 3000.0 + calibSum*10.0 - avgDist - fallbackPenalty - duplicatePenalty
			score -= productKindPenalty(productKind)

			if selectionMode == "single_filter_fallback" {
				score -= 600.0
			}

			if normalizedName(grp.TargetName) == normalizedName(inputTarget) {
				score += 500.0
			} else if strings.Contains(normalizedName(grp.TargetName), normalizedName(inputTarget)) ||
				strings.Contains(normalizedName(inputTarget), normalizedName(grp.TargetName)) {
				score += 250.0
			}

			if !strings.Contains(strings.ToUpper(grp.TargetName), "POSITION") &&
				!strings.Contains(strings.ToUpper(grp.TargetName), "MRS") {
				score += 75.0
			}

			if strings.TrimSpace(grp.ObservationID) != "" {
				score += 15.0
			}

			candidates = append(candidates, GroupCandidate{
				TargetName:       grp.TargetName,
				ObservationKey:   grp.ObservationKey,
				ObservationID:    grp.ObservationID,
				Files:            grp.Files,
				Channels:         channels,
				AvgDist:          avgDist,
				Score:            score,
				FallbackPenalty:  fallbackPenalty,
				DuplicatePenalty: duplicatePenalty,
				SelectionMode:    selectionMode,
				ProductKind:      productKind,
			})
		}
	}

	if len(candidates) == 0 {
		msg := "no usable RGB set found"
		if allowCalFits && allowSingleFilter {
			msg += " (checked observation-compatible _i2d.fits, _cal.fits, and single-filter fallback)"
		} else if allowCalFits {
			msg += " (checked observation-compatible _i2d.fits and _cal.fits)"
		} else {
			msg += " (checked observation-compatible _i2d.fits only)"
		}
		return GroupCandidate{}, errors.New(msg)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].FallbackPenalty == candidates[j].FallbackPenalty {
				if candidates[i].DuplicatePenalty == candidates[j].DuplicatePenalty {
					if candidates[i].AvgDist == candidates[j].AvgDist {
						if candidates[i].TargetName == candidates[j].TargetName {
							return candidates[i].ObservationID < candidates[j].ObservationID
						}
						return candidates[i].TargetName < candidates[j].TargetName
					}
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

func ChooseBestRGBSet(
	rows []map[string]any,
	inputTarget string,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
	allowSingleFilter bool,
	debug bool,
) (GroupCandidate, error) {
	return ChooseBest(rows, inputTarget, cfg, allowCalFits, allowSingleFilter, debug)
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
	observationKeys := map[string]int{}

	for _, row := range rows {
		dataURL := strings.TrimSpace(asString(row["dataURL"]))
		lower := strings.ToLower(dataURL)
		targetName := strings.TrimSpace(asString(row["target_name"]))

		if targetName != "" {
			targetNames[targetName]++
		}

		if key := compatibilityGroupKey(cfg, row); key != "" {
			observationKeys[key]++
		}

		for _, token := range parseFilterTokens(asString(row["filters"])) {
			s, ok := perFilter[token]
			if !ok {
				s = &stat{}
				perFilter[token] = s
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

	log.Printf("Debug observation groups found (%d unique)", len(observationKeys))

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
	requested := normalizeFilterToken(spec.PreferredFilters[0])

	for idx, filterName := range spec.PreferredFilters {
		filterName = normalizeFilterToken(filterName)
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

func betterRowForMatchedFilter(
	a, b map[string]any,
	targetFilter string,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
) bool {
	matchA := filterMatchSpecificity(a, targetFilter)
	matchB := filterMatchSpecificity(b, targetFilter)
	if matchA != matchB {
		return matchA > matchB
	}

	return betterRow(a, b, cfg, allowCalFits)
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

	expA := asFloat(a["t_exptime"])
	expB := asFloat(b["t_exptime"])
	if expA != expB {
		return expA > expB
	}

	distA := asFloat(a["distance"])
	distB := asFloat(b["distance"])
	return distA < distB
}

func filterMatchSpecificity(row map[string]any, targetFilter string) int {
	targetFilter = normalizeFilterToken(targetFilter)
	if targetFilter == "" {
		return 0
	}

	tokens := parseFilterTokens(asString(row["filters"]))
	if len(tokens) == 1 && tokens[0] == targetFilter {
		return 3
	}

	for _, token := range tokens {
		if token == targetFilter {
			return 2
		}
	}

	return 0
}

func matchedRequestedFilters(row map[string]any, wantedFilters map[string]bool) []string {
	seen := map[string]bool{}
	var out []string

	for _, token := range parseFilterTokens(asString(row["filters"])) {
		if !wantedFilters[token] {
			continue
		}
		if seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}

	sort.Strings(out)
	return out
}

func parseFilterTokens(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == ','
	})

	seen := map[string]bool{}
	var out []string
	for _, part := range parts {
		token := normalizeFilterToken(part)
		if token == "" {
			continue
		}
		if token == "N/A" || token == "NONE" {
			continue
		}
		if seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}

	return out
}

func normalizeFilterToken(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
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
	actual = normalizeFilterToken(actual)
	for i, f := range filters {
		if normalizeFilterToken(f) == actual {
			return i
		}
	}
	return len(filters) + 5
}

func uniqueFilterCount(filters ...string) int {
	seen := map[string]bool{}
	for _, f := range filters {
		f = normalizeFilterToken(f)
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
		f = normalizeFilterToken(f)
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
	s = strings.ReplaceAll(s, "/", "")
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

func compatibilityGroupKey(cfg rgb_configs.SourceConfig, row map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(cfg.Name)) {
	case "jwst":
		return jwstCompatibilityKey(row)
	case "hst":
		return hstObservationGroupKey(row)
	default:
		return ""
	}
}

func observationDisplayID(cfg rgb_configs.SourceConfig, row map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(cfg.Name)) {
	case "jwst":
		if v := firstNonEmptyField(row,
			"obs_id",
			"obsid",
			"observation_id",
			"observationid",
			"obsID",
			"observationID",
		); v != "" {
			return jwstFamilyDisplay(v)
		}
		if base := jwstBaseFileName(row); base != "" {
			return jwstFamilyDisplay(base)
		}
		return ""
	case "hst":
		if v := firstNonEmptyField(row,
			"obs_id",
			"obsid",
			"observation_id",
			"observationid",
			"obsID",
			"observationID",
		); v != "" {
			return v
		}
		return hstFileFamilyKey(row)
	default:
		return ""
	}
}

func firstNonEmptyField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		v := strings.TrimSpace(asString(row[key]))
		if v != "" {
			return v
		}
	}
	return ""
}

func jwstCompatibilityKey(row map[string]any) string {
	if v := firstNonEmptyField(row,
		"obs_id",
		"observation_id",
		"obsID",
		"observationID",
	); v != "" {
		return jwstFamilyKey(v, asString(row["instrument_name"]))
	}

	if base := jwstBaseFileName(row); base != "" {
		return jwstFamilyKey(base, asString(row["instrument_name"]))
	}

	return ""
}

func jwstFamilyKey(raw string, instrumentName string) string {
	display := jwstFamilyDisplay(raw)
	if display == "" {
		return ""
	}

	key := normalizeKey(display)

	parts := strings.Split(display, "_")
	if len(parts) >= 3 && containsLetters(parts[2]) {
		return key
	}

	inst := normalizeKey(instrumentName)
	if inst == "" {
		return key
	}

	return key + "|" + inst
}

func jwstFamilyDisplay(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}

	base := raw
	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}
	base = strings.TrimSuffix(base, ".fits")
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimSuffix(base, ".jpg")
	base = strings.TrimSuffix(base, ".csv")

	parts := strings.Split(base, "_")
	if len(parts) >= 3 {
		return strings.Join(parts[:3], "_")
	}
	if len(parts) >= 2 {
		return strings.Join(parts[:2], "_")
	}
	return base
}

func containsLetters(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func jwstBaseFileName(row map[string]any) string {
	base := strings.ToLower(strings.TrimSpace(asString(row["dataURL"])))
	if base == "" {
		return ""
	}

	if slash := strings.LastIndex(base, "/"); slash >= 0 {
		base = base[slash+1:]
	}

	base = strings.TrimSuffix(base, ".fits")
	for _, suffix := range []string{"_i2d", "_cal", "_rate", "_rateints", "_uncal", "_crf"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
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

func hstObservationGroupKey(row map[string]any) string {
	metaKey := hstCompatibilityKey(row)
	familyKey := hstFileFamilyKey(row)

	switch {
	case metaKey != "" && familyKey != "":
		return metaKey + "|" + familyKey
	case metaKey != "":
		return metaKey
	case familyKey != "":
		return familyKey
	default:
		return ""
	}
}

func rowsCompatibleForRGB(cfg rgb_configs.SourceConfig, rows ...map[string]any) bool {
	var firstKey string

	for _, row := range rows {
		key := compatibilityGroupKey(cfg, row)
		if key == "" {
			return false
		}
		if firstKey == "" {
			firstKey = key
			continue
		}
		if key != firstKey {
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
	case []byte:
		return string(t)
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
