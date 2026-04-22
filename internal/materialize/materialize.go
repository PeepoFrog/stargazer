package materialize

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

type ChannelChoice struct {
	RequestedFilter string
	ActualFilter    string
	FallbackRank    int
	ProductKind     string
	DataURL         string
}

type Candidate struct {
	TargetName string
	Channels   map[string]ChannelChoice
}

type Result struct {
	Files            map[string]string
	SelectedChannels map[string]any
}

type Dependencies struct {
	FileExists        func(path string) bool
	DownloadByDataURI func(client *http.Client, dataURI, savePath, label string) error
}

func MaterializeChannels(
	client *http.Client,
	fitsDir string,
	candidate Candidate,
	deps Dependencies,
) (Result, error) {
	if deps.DownloadByDataURI == nil {
		return Result{}, fmt.Errorf("materialize: DownloadByDataURI dependency is nil")
	}

	if err := os.MkdirAll(fitsDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("mkdir fits dir: %w", err)
	}

	files := map[string]string{}
	selectedChannels := map[string]any{}

	log.Printf("Materialize target=%q fits_dir=%q", candidate.TargetName, fitsDir)

	for _, channelName := range []string{"red", "green", "blue"} {
		choice, ok := candidate.Channels[channelName]
		if !ok {
			return Result{}, fmt.Errorf("missing %s channel in candidate", channelName)
		}
		if choice.DataURL == "" {
			return Result{}, fmt.Errorf("empty dataURL for %s channel using filter %s", channelName, choice.ActualFilter)
		}

		filename := filepath.Base(choice.DataURL)
		savePath := filepath.Join(fitsDir, filename)

		log.Printf(
			"Channel debug channel=%s requested=%s actual=%s fallback_rank=%d product_kind=%s data_url=%q save_path=%q",
			channelName,
			choice.RequestedFilter,
			choice.ActualFilter,
			choice.FallbackRank,
			choice.ProductKind,
			choice.DataURL,
			savePath,
		)

		if deps.FileExists != nil && deps.FileExists(savePath) {
			log.Printf(
				"Using cached %s channel file %s (filter=%s, requested=%s, fallback_rank=%d, product_kind=%s)",
				channelName,
				savePath,
				choice.ActualFilter,
				choice.RequestedFilter,
				choice.FallbackRank,
				choice.ProductKind,
			)
		} else {
			log.Printf(
				"Downloading %s channel using %s (requested %s, fallback_rank=%d, product_kind=%s) -> %s",
				channelName,
				choice.ActualFilter,
				choice.RequestedFilter,
				choice.FallbackRank,
				choice.ProductKind,
				savePath,
			)

			label := fmt.Sprintf("%s/%s", channelName, choice.ActualFilter)
			if err := deps.DownloadByDataURI(client, choice.DataURL, savePath, label); err != nil {
				return Result{}, fmt.Errorf(
					"download %s channel (%s) data_url=%q: %w",
					channelName,
					choice.ActualFilter,
					choice.DataURL,
					err,
				)
			}
		}

		files[channelName] = savePath
		selectedChannels[channelName] = map[string]any{
			"requested_filter": choice.RequestedFilter,
			"actual_filter":    choice.ActualFilter,
			"fallback_rank":    choice.FallbackRank,
			"product_kind":     choice.ProductKind,
			"data_url":         choice.DataURL,
		}
	}

	return Result{
		Files:            files,
		SelectedChannels: selectedChannels,
	}, nil
}

func MustMaterializeChannels(
	client *http.Client,
	fitsDir string,
	candidate Candidate,
	deps Dependencies,
) Result {
	result, err := MaterializeChannels(client, fitsDir, candidate, deps)
	if err != nil {
		log.Fatal(err)
	}
	return result
}
