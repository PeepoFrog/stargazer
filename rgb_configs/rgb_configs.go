package rgb_configs

import (
	"fmt"
	"strings"
)

type SourceConfig struct {
	Name              string
	ObsCollection     string
	InstrumentPattern string
	RGBSpecs          []RgbChannelSpec
}

type RgbChannelSpec struct {
	Name             string
	PreferredFilters []string
}

var jwstRGBSpecs = []RgbChannelSpec{
	{
		Name:             "red",
		PreferredFilters: []string{"F1130W", "F1280W", "F1500W", "F1800W", "F2100W", "F2550W", "F1000W", "F770W", "F560W"},
	},
	{
		Name:             "green",
		PreferredFilters: []string{"F1000W", "F1130W", "F770W", "F1280W", "F1500W", "F560W", "F1800W", "F2100W", "F2550W"},
	},
	{
		Name:             "blue",
		PreferredFilters: []string{"F770W", "F560W", "F1000W", "F1130W", "F1280W", "F1500W", "F1800W", "F2100W", "F2550W"},
	},
}

var hstRGBSpecs = []RgbChannelSpec{
	{
		Name: "red",
		PreferredFilters: []string{
			"F814W", "F850LP", "F775W", "F625W", "F606W", "F555W", "F547M",
			"F160W", "F140W", "F125W", "F110W",
		},
	},
	{
		Name: "green",
		PreferredFilters: []string{
			"F606W", "F555W", "F547M", "F625W", "F775W", "F814W",
			"F125W", "F110W",
		},
	},
	{
		Name: "blue",
		PreferredFilters: []string{
			"F438W", "F435W", "F475W", "F390W", "F336W", "F300X",
			"F606W", "F555W",
		},
	},
}

func GetSourceConfig(source string) (SourceConfig, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "jwst":
		return SourceConfig{
			Name:              "jwst",
			ObsCollection:     "JWST",
			InstrumentPattern: "%MIRI%",
			RGBSpecs:          jwstRGBSpecs,
		}, nil
	case "hst":
		return SourceConfig{
			Name:              "hst",
			ObsCollection:     "HST",
			InstrumentPattern: "",
			RGBSpecs:          hstRGBSpecs,
		}, nil
	default:
		return SourceConfig{}, fmt.Errorf("unsupported -source %q (use jwst or hst)", source)
	}
}
