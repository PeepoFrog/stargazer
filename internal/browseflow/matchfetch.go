package browseflow

import (
	"net/http"

	"github.com/PeepFrog/datastsciparser/internal/mastapi"
	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

func FetchRowsFromMAST(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	opts BrowseOptions,
) ([]map[string]any, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 300
	}

	page := 1
	if opts.Offset > 0 {
		page = (opts.Offset / limit) + 1
	}

	return mastapi.SearchImagesCatalog(
		client,
		cfg,
		page,
		limit,
		opts.DebugSelection,
	)
}
