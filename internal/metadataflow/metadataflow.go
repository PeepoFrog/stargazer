package metadataflow

import (
	"encoding/json"
	"log"
	"os"

	"github.com/PeepFrog/datastsciparser/internal/cli"
	"github.com/PeepFrog/datastsciparser/internal/materialize"
	"github.com/PeepFrog/datastsciparser/internal/outputlayout"
	"github.com/PeepFrog/datastsciparser/internal/renderflow"
	"github.com/PeepFrog/datastsciparser/internal/searchflow"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

func Build(
	searchResult searchflow.Result,
	selectedTarget string,
	selectionMode string,
	productKind string,
	preset renderflow.Preset,
	materialized materialize.Result,
	layout outputlayout.Layout,
	cfg rgb_configs.SourceConfig,
	opts cli.Options,
) map[string]any {
	return map[string]any{
		"input_target":        searchResult.SearchInput,
		"canonical_name":      searchResult.CanonicalName,
		"selected_target":     selectedTarget,
		"preset":              preset,
		"selected_channels":   materialized.SelectedChannels,
		"files":               materialized.Files,
		"search_mode":         searchResult.SearchMode,
		"selection_mode":      selectionMode,
		"product_kind":        productKind,
		"allow_cal_fits":      opts.AllowCalFits,
		"allow_single_filter": opts.AllowSingleFilter,
		"source":              cfg.Name,
		"telescope_dir":       layout.TelescopeDir,
		"object_dir":          layout.ObjectDir,
	}
}

func MustWriteJSON(path string, v any) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("write metadata: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		log.Fatalf("write metadata: %v", err)
	}
}
