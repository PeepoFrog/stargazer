package helper

import (
	"os"

	"github.com/PeepFrog/datastsciparser/internal/materialize"
	"github.com/PeepFrog/datastsciparser/internal/selectionengine"
)

func ToMaterializeCandidate(best selectionengine.GroupCandidate) materialize.Candidate {
	channels := map[string]materialize.ChannelChoice{}

	for _, channelName := range []string{"red", "green", "blue"} {
		choice := best.Channels[channelName]
		channels[channelName] = materialize.ChannelChoice{
			RequestedFilter: choice.RequestedFilter,
			ActualFilter:    choice.ActualFilter,
			FallbackRank:    choice.FallbackRank,
			ProductKind:     choice.ProductKind,
			DataURL:         choice.DataURL,
		}
	}

	return materialize.Candidate{
		TargetName: best.TargetName,
		Channels:   channels,
	}
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Size() > 0
}
