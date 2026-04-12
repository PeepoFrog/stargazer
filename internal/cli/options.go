package cli

import (
	"flag"
	"log"
	"strings"
)

type Options struct {
	Source string

	Target          string
	TargetNameExact string

	RadiusDeg float64
	OutDir    string
	Verbose   bool

	PresetName  string
	StretchMode string

	RGain float64
	GGain float64
	BGain float64

	RLow  float64
	RHigh float64
	GLow  float64
	GHigh float64
	BLow  float64
	BHigh float64

	AllowCalFits      bool
	AllowSingleFilter bool
	DebugSelection    bool
}

func MustParseOptions() Options {
	source := flag.String("source", "", "required: jwst or hst")

	target := flag.String("target", "", "resolvable object name, e.g. 'NGC 6720'")
	targetNameExact := flag.String("target-name-exact", "", "exact MAST target_name to search without name resolving")

	radiusDeg := flag.Float64("radius", 0.03, "search radius in degrees")
	outDir := flag.String("out", "./cli_output", "output directory")
	verbose := flag.Bool("verbose", false, "verbose HTTP logs")

	presetName := flag.String("preset-name", "default_v1", "preset name written to metadata/output filename")
	stretchMode := flag.String("mode", "sqrt", "stretch mode: linear, sqrt, log, asinh")

	rGain := flag.Float64("r-gain", 1.00, "red channel gain")
	gGain := flag.Float64("g-gain", 1.00, "green channel gain")
	bGain := flag.Float64("b-gain", 1.10, "blue channel gain")

	rLow := flag.Float64("r-low", 0.20, "red lower percentile")
	rHigh := flag.Float64("r-high", 99.7, "red upper percentile")
	gLow := flag.Float64("g-low", 0.30, "green lower percentile")
	gHigh := flag.Float64("g-high", 99.3, "green upper percentile")
	bLow := flag.Float64("b-low", 0.20, "blue lower percentile")
	bHigh := flag.Float64("b-high", 99.7, "blue upper percentile")

	allowCalFits := flag.Bool("allow-cal-fits", true, "allow fallback to _cal.fits when _i2d.fits is unavailable")
	allowSingleFilter := flag.Bool("allow-single-filter", true, "allow fallback render when only one usable filter exists")
	debugSelection := flag.Bool("debug-selection", false, "log selection summary and row skip reasons")

	flag.Parse()

	opts := Options{
		Source: strings.TrimSpace(*source),

		Target:          strings.TrimSpace(*target),
		TargetNameExact: strings.TrimSpace(*targetNameExact),

		RadiusDeg: *radiusDeg,
		OutDir:    *outDir,
		Verbose:   *verbose,

		PresetName:  *presetName,
		StretchMode: *stretchMode,

		RGain: *rGain,
		GGain: *gGain,
		BGain: *bGain,

		RLow:  *rLow,
		RHigh: *rHigh,
		GLow:  *gLow,
		GHigh: *gHigh,
		BLow:  *bLow,
		BHigh: *bHigh,

		AllowCalFits:      *allowCalFits,
		AllowSingleFilter: *allowSingleFilter,
		DebugSelection:    *debugSelection,
	}

	validateOrExit(opts)
	return opts
}

func validateOrExit(opts Options) {
	if opts.Source == "" {
		flag.Usage()
		log.Fatal("please specify -source jwst or -source hst")
	}

	if opts.Target == "" && opts.TargetNameExact == "" {
		log.Fatal("missing required target: use either -target or -target-name-exact")
	}

	if opts.Target != "" && opts.TargetNameExact != "" {
		log.Fatal("use only one of -target or -target-name-exact")
	}
}

func (o Options) SearchInput() string {
	if o.TargetNameExact != "" {
		return o.TargetNameExact
	}
	return o.Target
}

func (o Options) SearchMode() string {
	if o.TargetNameExact != "" {
		return "target_name_exact"
	}
	return "resolved_name"
}
