package selectionflow

import (
	"log"

	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

type ChannelSelection struct {
	Channel         string         `json:"channel"`
	RequestedFilter string         `json:"requested_filter"`
	ActualFilter    string         `json:"actual_filter"`
	FallbackRank    int            `json:"fallback_rank"`
	ProductKind     string         `json:"product_kind"`
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

type ChooseBestFunc func(
	rows []map[string]any,
	inputTarget string,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
	allowSingleFilter bool,
	debug bool,
) (GroupCandidate, error)

func MustChooseBest(
	rows []map[string]any,
	inputTarget string,
	cfg rgb_configs.SourceConfig,
	allowCalFits bool,
	allowSingleFilter bool,
	debug bool,
	choose ChooseBestFunc,
) GroupCandidate {
	best, err := choose(
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
