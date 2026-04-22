package main

import (
	"flag"
	"log"
	"strings"

	"github.com/PeepFrog/datastsciparser/internal/catalogdb"
	"github.com/PeepFrog/datastsciparser/internal/renderflow"
	"github.com/PeepFrog/datastsciparser/internal/runflow"
	"github.com/PeepFrog/datastsciparser/internal/userdatapath"
)

func main() {
	var (
		source     = flag.String("source", "jwst", "data source: jwst|hst")
		target     = flag.String("target", "", "exact catalog target name")
		outDir     = flag.String("out", "./cli_output", "output directory")
		presetName = flag.String("preset-name", "default_v1", "preset name")
		mode       = flag.String("mode", "sqrt", "stretch mode: linear, sqrt, log, asinh")

		rGain = flag.Float64("r-gain", 1.00, "red channel gain")
		gGain = flag.Float64("g-gain", 1.00, "green channel gain")
		bGain = flag.Float64("b-gain", 1.10, "blue channel gain")

		rLow  = flag.Float64("r-low", 0.20, "red lower percentile")
		rHigh = flag.Float64("r-high", 99.7, "red upper percentile")
		gLow  = flag.Float64("g-low", 0.30, "green lower percentile")
		gHigh = flag.Float64("g-high", 99.3, "green upper percentile")
		bLow  = flag.Float64("b-low", 0.20, "blue lower percentile")
		bHigh = flag.Float64("b-high", 99.7, "blue upper percentile")
	)
	flag.Parse()

	if strings.TrimSpace(*target) == "" {
		log.Fatal("missing required -target")
	}

	dbPath, err := userdatapath.CatalogDBPath(*source)
	if err != nil {
		log.Fatalf("resolve db path: %v", err)
	}

	store, err := catalogdb.Open(dbPath)
	if err != nil {
		log.Fatalf("open catalog db: %v", err)
	}
	defer store.Close()

	rec, ok, err := store.GetCandidate(*source, *target)
	if err != nil {
		log.Fatalf("get candidate: %v", err)
	}
	if !ok {
		log.Fatalf("candidate not found for source=%q target=%q", *source, *target)
	}

	log.Printf(
		"Candidate debug source=%q target=%q classification=%q obs_id=%q obs_key=%q quality=%q product=%q",
		rec.Source,
		rec.TargetName,
		rec.TargetClassification,
		rec.ObservationID,
		rec.ObservationKey,
		rec.Quality,
		rec.ProductKind,
	)

	log.Printf(
		"Candidate URLs red_filter=%q red_url=%q",
		rec.RedFilter,
		rec.RedDataURL,
	)
	log.Printf(
		"Candidate URLs green_filter=%q green_url=%q",
		rec.GreenFilter,
		rec.GreenDataURL,
	)
	log.Printf(
		"Candidate URLs blue_filter=%q blue_url=%q",
		rec.BlueFilter,
		rec.BlueDataURL,
	)

	preset := renderflow.Preset{
		Name:  *presetName,
		Mode:  *mode,
		RLow:  *rLow,
		RHigh: *rHigh,
		RGain: *rGain,
		GLow:  *gLow,
		GHigh: *gHigh,
		GGain: *gGain,
		BLow:  *bLow,
		BHigh: *bHigh,
		BGain: *bGain,
	}

	req, err := runflow.BuildRequestFromCatalogCandidate(*source, rec, *outDir, preset)
	if err != nil {
		log.Fatalf("build run request: %v", err)
	}

	for _, key := range []string{"red", "green", "blue"} {
		ch := req.Channels[key]
		log.Printf(
			"RunRequest channel=%s requested=%q actual=%q product=%q data_url=%q",
			key,
			ch.RequestedFilter,
			ch.ActualFilter,
			ch.ProductKind,
			ch.DataURL,
		)
	}

	result, err := runflow.RunCandidate(req)
	if err != nil {
		log.Fatalf("run candidate: %v", err)
	}

	log.Printf("Saved image: %s", result.Layout.ImagePath)
	log.Printf("Saved metadata: %s", result.Layout.MetadataPath)
}
