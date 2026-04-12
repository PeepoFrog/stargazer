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

func MustMaterializeChannels(
	client *http.Client,
	fitsDir string,
	candidate Candidate,
	deps Dependencies,
) Result {
	if deps.DownloadByDataURI == nil {
		log.Fatal("materialize: DownloadByDataURI dependency is nil")
	}

	if err := os.MkdirAll(fitsDir, 0o755); err != nil {
		log.Fatalf("mkdir fits dir: %v", err)
	}

	files := map[string]string{}
	selectedChannels := map[string]any{}

	for _, channelName := range []string{"red", "green", "blue"} {
		choice, ok := candidate.Channels[channelName]
		if !ok {
			log.Fatalf("missing %s channel in candidate", channelName)
		}
		if choice.DataURL == "" {
			log.Fatalf("empty dataURL for %s channel using filter %s", channelName, choice.ActualFilter)
		}

		filename := filepath.Base(choice.DataURL)
		savePath := filepath.Join(fitsDir, filename)

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
				log.Fatalf("download %s channel (%s): %v", channelName, choice.ActualFilter, err)
			}
		}

		files[channelName] = savePath
		selectedChannels[channelName] = map[string]any{
			"requested_filter": choice.RequestedFilter,
			"actual_filter":    choice.ActualFilter,
			"fallback_rank":    choice.FallbackRank,
			"product_kind":     choice.ProductKind,
		}
	}

	return Result{
		Files:            files,
		SelectedChannels: selectedChannels,
	}
}
