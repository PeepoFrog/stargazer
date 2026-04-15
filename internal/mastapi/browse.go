package mastapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

func SearchImagesCatalog(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	page int,
	pageSize int,
	verbose bool,
) ([]map[string]any, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 300
	}

	filters := []map[string]any{
		exactFilter("obs_collection", cfg.ObsCollection),
		exactFilter("dataproduct_type", "image"),
	}
	if strings.TrimSpace(cfg.InstrumentPattern) != "" {
		filters = append(filters, freeTextFilter("instrument_name", cfg.InstrumentPattern))
	}

	req := Request{
		Service:  "Mast.Caom.Filtered",
		Format:   "json",
		Page:     page,
		PageSize: pageSize,
		Params: map[string]any{
			"columns": "*",
			"filters": filters,
		},
		RemoveNullColumns: true,
		RemoveCache:       true,
	}

	var resp Response
	if err := invoke(client, req, &resp, verbose); err != nil {
		return nil, fmt.Errorf("search browse catalog: %w", err)
	}

	return resp.Data, nil
}
