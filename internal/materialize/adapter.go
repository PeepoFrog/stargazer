package materialize

import (
	"fmt"

	"github.com/PeepFrog/datastsciparser/internal/selectionflow"
)

func FromSelectionCandidate(best selectionflow.GroupCandidate) Candidate {
	channels := map[string]ChannelChoice{}

	for _, channelName := range []string{"red", "green", "blue"} {
		choice := best.Channels[channelName]
		channels[channelName] = ChannelChoice{
			RequestedFilter: choice.RequestedFilter,
			ActualFilter:    choice.ActualFilter,
			FallbackRank:    choice.FallbackRank,
			ProductKind:     choice.ProductKind,
			DataURL:         asString(choice.Row["dataURL"]),
		}
	}

	return Candidate{
		TargetName: best.TargetName,
		Channels:   channels,
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
