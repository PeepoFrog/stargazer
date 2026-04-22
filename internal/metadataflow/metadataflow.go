package metadataflow

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/PeepFrog/datastsciparser/internal/cli"
	"github.com/PeepFrog/datastsciparser/internal/materialize"
	"github.com/PeepFrog/datastsciparser/internal/outputlayout"
	"github.com/PeepFrog/datastsciparser/internal/renderflow"
	"github.com/PeepFrog/datastsciparser/internal/searchflow"
	"github.com/PeepFrog/datastsciparser/internal/selectionengine"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

func Build(
	searchResult searchflow.Result,
	best selectionengine.GroupCandidate,
	preset renderflow.Preset,
	materialized materialize.Result,
	layout outputlayout.Layout,
	cfg rgb_configs.SourceConfig,
	opts cli.Options,
) map[string]any {
	return map[string]any{
		"input_target":        searchResult.SearchInput,
		"canonical_name":      searchResult.CanonicalName,
		"selected_target":     best.TargetName,
		"observation_id":      best.ObservationID,
		"observation_key":     best.ObservationKey,
		"preset":              preset,
		"selected_channels":   materialized.SelectedChannels,
		"files":               materialized.Files,
		"search_mode":         searchResult.SearchMode,
		"search_summary":      searchResult.Summary,
		"selection_mode":      best.SelectionMode,
		"product_kind":        best.ProductKind,
		"allow_cal_fits":      opts.AllowCalFits,
		"allow_single_filter": opts.AllowSingleFilter,
		"source":              cfg.Name,
		"telescope_dir":       layout.TelescopeDir,
		"object_dir":          layout.ObjectDir,
		"winner_candidate": map[string]any{
			"target_name":        best.TargetName,
			"observation_id":     best.ObservationID,
			"observation_key":    best.ObservationKey,
			"score":              best.Score,
			"avg_distance":       best.AvgDist,
			"fallback_penalty":   best.FallbackPenalty,
			"duplicate_penalty":  best.DuplicatePenalty,
			"selection_mode":     best.SelectionMode,
			"product_kind":       best.ProductKind,
			"selected_channels":  materialized.SelectedChannels,
			"materialized_files": materialized.Files,
		},
	}
}

func BuildCandidateRun(
	source string,
	targetName string,
	targetClassification string,
	observationID string,
	observationKey string,
	preset renderflow.Preset,
	materialized materialize.Result,
	layout outputlayout.Layout,
	cfg rgb_configs.SourceConfig,
	renderInfo renderflow.RenderInfo,
) map[string]any {
	return map[string]any{
		"source":                source,
		"source_config_name":    cfg.Name,
		"target_name":           targetName,
		"target_classification": targetClassification,
		"observation_id":        observationID,
		"observation_key":       observationKey,
		"preset":                preset,
		"selected_channels":     materialized.SelectedChannels,
		"files":                 materialized.Files,
		"render_info":           renderInfo,
		"telescope_dir":         layout.TelescopeDir,
		"object_dir":            layout.ObjectDir,
		"image_path":            layout.ImagePath,
		"metadata_path":         layout.MetadataPath,
	}
}

func WriteJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create metadata file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode metadata json: %w", err)
	}
	return nil
}

func MustWriteJSON(path string, v any) {
	if err := WriteJSON(path, v); err != nil {
		panic(err)
	}
}
