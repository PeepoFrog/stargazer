package mastapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/PeepFrog/datastsciparser/rgb_configs"
)

const invokeURL = "https://mast.stsci.edu/api/v0/invoke"

type Request struct {
	Service           string         `json:"service"`
	Params            map[string]any `json:"params,omitempty"`
	Format            string         `json:"format,omitempty"`
	PageSize          int            `json:"pagesize,omitempty"`
	Page              int            `json:"page,omitempty"`
	RemoveNullColumns bool           `json:"removenullcolumns,omitempty"`
	RemoveCache       bool           `json:"removecache,omitempty"`
}

type Response struct {
	Status string           `json:"status"`
	Msg    string           `json:"msg"`
	Data   []map[string]any `json:"data"`
	Fields []map[string]any `json:"fields"`
	Paging map[string]any   `json:"paging"`
}

type ResolverResponse struct {
	ResolvedCoordinate []struct {
		CanonicalName string  `json:"canonicalName"`
		RA            float64 `json:"ra"`
		Dec           float64 `json:"decl"`
	} `json:"resolvedCoordinate"`
	Status string `json:"status"`
}

func ResolveName(client *http.Client, name string, verbose bool) (string, float64, float64, error) {
	req := Request{
		Service: "Mast.Name.Lookup",
		Params: map[string]any{
			"input":  name,
			"format": "json",
		},
	}

	var resp ResolverResponse
	if err := invoke(client, req, &resp, verbose); err != nil {
		return "", 0, 0, err
	}
	if len(resp.ResolvedCoordinate) == 0 {
		return "", 0, 0, fmt.Errorf("no coordinates resolved for %q", name)
	}

	return resp.ResolvedCoordinate[0].CanonicalName, resp.ResolvedCoordinate[0].RA, resp.ResolvedCoordinate[0].Dec, nil
}

func SearchImagesByPosition(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	ra, dec, radiusDeg float64,
	verbose bool,
) ([]map[string]any, error) {
	position := fmt.Sprintf("%.8f, %.8f, %.6f", ra, dec, radiusDeg)

	filters := []map[string]any{
		exactFilter("obs_collection", cfg.ObsCollection),
		exactFilter("dataproduct_type", "image"),
	}
	if strings.TrimSpace(cfg.InstrumentPattern) != "" {
		filters = append(filters, freeTextFilter("instrument_name", cfg.InstrumentPattern))
	}

	req := Request{
		Service:  "Mast.Caom.Filtered.Position",
		Format:   "json",
		Page:     1,
		PageSize: 300,
		Params: map[string]any{
			"columns":  "*",
			"filters":  filters,
			"position": position,
		},
		RemoveNullColumns: true,
		RemoveCache:       true,
	}

	var resp Response
	if err := invoke(client, req, &resp, verbose); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func SearchImagesByExactTargetName(
	client *http.Client,
	cfg rgb_configs.SourceConfig,
	targetName string,
	verbose bool,
) ([]map[string]any, error) {
	filters := []map[string]any{
		exactFilter("obs_collection", cfg.ObsCollection),
		exactFilter("dataproduct_type", "image"),
		exactFilter("target_name", targetName),
	}
	if strings.TrimSpace(cfg.InstrumentPattern) != "" {
		filters = append(filters, freeTextFilter("instrument_name", cfg.InstrumentPattern))
	}

	req := Request{
		Service:  "Mast.Caom.Filtered",
		Format:   "json",
		Page:     1,
		PageSize: 300,
		Params: map[string]any{
			"columns": "*",
			"filters": filters,
		},
		RemoveNullColumns: true,
		RemoveCache:       true,
	}

	var resp Response
	if err := invoke(client, req, &resp, verbose); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func exactFilter(param string, values ...string) map[string]any {
	return map[string]any{
		"paramName": param,
		"values":    values,
	}
}

func freeTextFilter(param, pattern string) map[string]any {
	return map[string]any{
		"paramName": param,
		"values":    []string{},
		"freeText":  pattern,
	}
}

func invoke(client *http.Client, reqObj any, out any, verbose bool) error {
	raw, err := json.Marshal(reqObj)
	if err != nil {
		return err
	}

	form := url.Values{}
	form.Set("request", string(raw))

	req, err := http.NewRequest(http.MethodPost, invokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "go-object-rgb-cli/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if verbose {
		log.Printf("HTTP STATUS: %s", resp.Status)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invoke status %s: %s", resp.Status, string(body))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w; body=%s", err, string(body))
	}
	return nil
}
