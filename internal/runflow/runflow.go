package runflow

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/PeepFrog/datastsciparser/internal/catalogdb"
	"github.com/PeepFrog/datastsciparser/internal/cli"
	"github.com/PeepFrog/datastsciparser/internal/clients"
	"github.com/PeepFrog/datastsciparser/internal/downloader"
	"github.com/PeepFrog/datastsciparser/internal/mastapi"
	"github.com/PeepFrog/datastsciparser/internal/materialize"
	"github.com/PeepFrog/datastsciparser/internal/metadataflow"
	"github.com/PeepFrog/datastsciparser/internal/outputlayout"
	"github.com/PeepFrog/datastsciparser/internal/presetflow"
	"github.com/PeepFrog/datastsciparser/internal/renderflow"
	"github.com/PeepFrog/datastsciparser/internal/searchflow"
	"github.com/PeepFrog/datastsciparser/internal/selectionengine"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

type RunRequest struct {
	Source string
	OutDir string

	TargetName           string
	TargetClassification string
	ObservationID        string
	ObservationKey       string

	Preset   renderflow.Preset
	Channels map[string]materialize.ChannelChoice
}

type Result struct {
	Request       RunRequest
	SearchResult  searchflow.Result
	BestCandidate selectionengine.GroupCandidate
	Preset        renderflow.Preset
	Materialized  materialize.Result
	Layout        outputlayout.Layout
	Metadata      map[string]any
	SourceConfig  rgb_configs.SourceConfig
	RenderInfo    renderflow.RenderInfo
}

func DefaultPreset() renderflow.Preset {
	return renderflow.Preset{
		Name:  "default_v1",
		Mode:  "sqrt",
		RLow:  0.20,
		RHigh: 99.7,
		RGain: 1.00,
		GLow:  0.30,
		GHigh: 99.3,
		GGain: 1.00,
		BLow:  0.20,
		BHigh: 99.7,
		BGain: 1.10,
	}
}

func BuildRequestFromCatalogCandidate(
	source string,
	rec catalogdb.CandidateRecord,
	outDir string,
	preset renderflow.Preset,
) (RunRequest, error) {
	if preset.Name == "" {
		preset = DefaultPreset()
	}

	req := RunRequest{
		Source:               strings.ToLower(strings.TrimSpace(source)),
		OutDir:               strings.TrimSpace(outDir),
		TargetName:           strings.TrimSpace(rec.TargetName),
		TargetClassification: strings.TrimSpace(rec.TargetClassification),
		ObservationID:        strings.TrimSpace(rec.ObservationID),
		ObservationKey:       strings.TrimSpace(rec.ObservationKey),
		Preset:               preset,
		Channels: map[string]materialize.ChannelChoice{
			"red": {
				RequestedFilter: rec.RedFilter,
				ActualFilter:    rec.RedFilter,
				FallbackRank:    0,
				ProductKind:     rec.ProductKind,
				DataURL:         rec.RedDataURL,
			},
			"green": {
				RequestedFilter: rec.GreenFilter,
				ActualFilter:    rec.GreenFilter,
				FallbackRank:    0,
				ProductKind:     rec.ProductKind,
				DataURL:         rec.GreenDataURL,
			},
			"blue": {
				RequestedFilter: rec.BlueFilter,
				ActualFilter:    rec.BlueFilter,
				FallbackRank:    0,
				ProductKind:     rec.ProductKind,
				DataURL:         rec.BlueDataURL,
			},
		},
	}

	return req, validateRequest(req)
}

func BuildRequestFromSelectedCandidate(
	source string,
	best selectionengine.GroupCandidate,
	outDir string,
	preset renderflow.Preset,
) (RunRequest, error) {
	if preset.Name == "" {
		preset = DefaultPreset()
	}

	req := RunRequest{
		Source:         strings.ToLower(strings.TrimSpace(source)),
		OutDir:         strings.TrimSpace(outDir),
		TargetName:     strings.TrimSpace(best.TargetName),
		ObservationID:  strings.TrimSpace(best.ObservationID),
		ObservationKey: strings.TrimSpace(best.ObservationKey),
		Preset:         preset,
		Channels:       map[string]materialize.ChannelChoice{},
	}

	for _, key := range []string{"red", "green", "blue"} {
		ch, ok := best.Channels[key]
		if !ok {
			return RunRequest{}, fmt.Errorf("missing %s channel in selected candidate", key)
		}
		req.Channels[key] = materialize.ChannelChoice{
			RequestedFilter: ch.RequestedFilter,
			ActualFilter:    ch.ActualFilter,
			FallbackRank:    ch.FallbackRank,
			ProductKind:     ch.ProductKind,
			DataURL:         ch.DataURL,
		}
	}

	return req, validateRequest(req)
}

func RunCandidate(req RunRequest) (Result, error) {
	if err := validateRequest(req); err != nil {
		return Result{}, err
	}

	cfg, err := rgb_configs.GetSourceConfig(req.Source)
	if err != nil {
		return Result{}, fmt.Errorf("get source config: %w", err)
	}

	clientSet := clients.New()
	layout := outputlayout.Build(req.OutDir, cfg.Name, req.TargetName, req.Preset.Name)

	materialized, err := materialize.MaterializeChannels(
		clientSet.Download,
		layout.FitsDir,
		materialize.Candidate{
			TargetName: req.TargetName,
			Channels:   req.Channels,
		},
		materialize.Dependencies{
			FileExists:        fileExists,
			DownloadByDataURI: downloader.DownloadByDataURI,
		},
	)
	if err != nil {
		return Result{}, err
	}

	renderInfo, err := renderflow.RenderAndSave(materialized, req.Preset, layout)
	if err != nil {
		return Result{}, err
	}
	log.Printf("Saved image: %s", layout.ImagePath)

	meta := metadataflow.BuildCandidateRun(
		req.Source,
		req.TargetName,
		req.TargetClassification,
		req.ObservationID,
		req.ObservationKey,
		req.Preset,
		materialized,
		layout,
		cfg,
		renderInfo,
	)

	if err := metadataflow.WriteJSON(layout.MetadataPath, meta); err != nil {
		return Result{}, err
	}
	log.Printf("Saved metadata: %s", layout.MetadataPath)

	return Result{
		Request:      req,
		Preset:       req.Preset,
		Materialized: materialized,
		Layout:       layout,
		Metadata:     meta,
		SourceConfig: cfg,
		RenderInfo:   renderInfo,
	}, nil
}

func MustRun(opts cli.Options) Result {
	cfg, err := rgb_configs.GetSourceConfig(opts.Source)
	if err != nil {
		panic(err)
	}

	clientSet := clients.New()
	preset := presetflow.Build(opts)

	searchResult := searchflow.MustSearch(
		clientSet.Meta,
		cfg,
		opts,
		searchflow.Dependencies{
			ResolveName:             mastapi.ResolveName,
			SearchImagesByPosition:  mastapi.SearchImagesByPosition,
			SearchImagesByExactName: mastapi.SearchImagesByExactTargetName,
			LogTargetRowsSummary:    selectionengine.LogTargetRowsSummary,
		},
	)

	best := selectionengine.MustChooseBest(
		searchResult.Rows,
		searchResult.SearchInput,
		cfg,
		opts.AllowCalFits,
		opts.AllowSingleFilter,
		opts.DebugSelection,
	)

	req, err := BuildRequestFromSelectedCandidate(opts.Source, best, opts.OutDir, preset)
	if err != nil {
		panic(err)
	}

	result, err := RunCandidate(req)
	if err != nil {
		panic(err)
	}

	result.SearchResult = searchResult
	result.BestCandidate = best

	result.Metadata["input_target"] = searchResult.SearchInput
	result.Metadata["canonical_name"] = searchResult.CanonicalName
	result.Metadata["search_mode"] = searchResult.SearchMode
	result.Metadata["search_summary"] = searchResult.Summary
	result.Metadata["selection_mode"] = best.SelectionMode
	result.Metadata["product_kind"] = best.ProductKind
	result.Metadata["winner_candidate"] = map[string]any{
		"target_name":       best.TargetName,
		"observation_id":    best.ObservationID,
		"observation_key":   best.ObservationKey,
		"score":             best.Score,
		"avg_distance":      best.AvgDist,
		"fallback_penalty":  best.FallbackPenalty,
		"duplicate_penalty": best.DuplicatePenalty,
		"selection_mode":    best.SelectionMode,
		"product_kind":      best.ProductKind,
	}

	if err := metadataflow.WriteJSON(result.Layout.MetadataPath, result.Metadata); err != nil {
		panic(err)
	}

	return result
}

func validateRequest(req RunRequest) error {
	if strings.TrimSpace(req.Source) == "" {
		return fmt.Errorf("run request: missing source")
	}
	if strings.TrimSpace(req.TargetName) == "" {
		return fmt.Errorf("run request: missing target name")
	}
	if strings.TrimSpace(req.OutDir) == "" {
		return fmt.Errorf("run request: missing out dir")
	}

	for _, key := range []string{"red", "green", "blue"} {
		ch, ok := req.Channels[key]
		if !ok {
			return fmt.Errorf("run request: missing %s channel", key)
		}
		if strings.TrimSpace(ch.DataURL) == "" {
			return fmt.Errorf("run request: missing dataURL for %s channel", key)
		}
		if strings.TrimSpace(ch.ActualFilter) == "" {
			return fmt.Errorf("run request: missing filter for %s channel", key)
		}
	}

	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Size() > 0
}
