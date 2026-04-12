package main

import (
	"github.com/PeepFrog/datastsciparser/internal/cli"
	"github.com/PeepFrog/datastsciparser/internal/clients"
	"github.com/PeepFrog/datastsciparser/internal/downloader"
	"github.com/PeepFrog/datastsciparser/internal/helper"
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

func main() {
	opts := cli.MustParseOptions()

	cfg, err := rgb_configs.GetSourceConfig(opts.Source)
	if err != nil {
		panic(err)
	}

	clientSet := clients.New()
	preset := presetflow.Build(opts)

	searchResult := searchflow.MustResolveAndSearch(
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

	layout := outputlayout.Build(opts.OutDir, cfg.Name, best.TargetName, preset.Name)

	materialized := materialize.MustMaterializeChannels(
		clientSet.Download,
		layout.FitsDir,
		helper.ToMaterializeCandidate(best),
		materialize.Dependencies{
			FileExists:        helper.FileExists,
			DownloadByDataURI: downloader.DownloadByDataURI,
		},
	)

	renderflow.MustRenderAndSave(materialized, preset, layout)

	meta := metadataflow.Build(
		searchResult,
		best.TargetName,
		best.SelectionMode,
		best.ProductKind,
		preset,
		materialized,
		layout,
		cfg,
		opts,
	)

	metadataflow.MustWriteJSON(layout.MetadataPath, meta)
}
