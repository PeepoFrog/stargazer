package presetflow

import (
	"github.com/PeepFrog/datastsciparser/internal/cli"
	"github.com/PeepFrog/datastsciparser/internal/renderflow"
)

func Build(opts cli.Options) renderflow.Preset {
	return renderflow.Preset{
		Name:  opts.PresetName,
		Mode:  opts.StretchMode,
		RLow:  opts.RLow,
		RHigh: opts.RHigh,
		RGain: opts.RGain,
		GLow:  opts.GLow,
		GHigh: opts.GHigh,
		GGain: opts.GGain,
		BLow:  opts.BLow,
		BHigh: opts.BHigh,
		BGain: opts.BGain,
	}
}
