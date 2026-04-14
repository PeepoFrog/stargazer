package renderflow

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"sort"

	"codeberg.org/astrogo/fitsio"
	"github.com/PeepFrog/datastsciparser/internal/materialize"
	"github.com/PeepFrog/datastsciparser/internal/outputlayout"
)

type Preset struct {
	Name  string  `json:"name"`
	Mode  string  `json:"mode"`
	RLow  float64 `json:"r_low"`
	RHigh float64 `json:"r_high"`
	RGain float64 `json:"r_gain"`
	GLow  float64 `json:"g_low"`
	GHigh float64 `json:"g_high"`
	GGain float64 `json:"g_gain"`
	BLow  float64 `json:"b_low"`
	BHigh float64 `json:"b_high"`
	BGain float64 `json:"b_gain"`
}
type GrayImage struct {
	W      int
	H      int
	Pix    []float64
	Sorted []float64
}

func readPixelsAsFloat64(img fitsio.Image, n int) ([]float64, error) {
	bitpix := img.Header().Bitpix()

	switch bitpix {
	case 8:
		buf := make([]int8, n)
		if err := img.Read(&buf); err != nil {
			return nil, err
		}
		out := make([]float64, n)
		for i, v := range buf {
			out[i] = float64(uint8(v))
		}
		return out, nil
	case 16:
		buf := make([]int16, n)
		if err := img.Read(&buf); err != nil {
			return nil, err
		}
		out := make([]float64, n)
		for i, v := range buf {
			out[i] = float64(v)
		}
		return out, nil
	case 32:
		buf := make([]int32, n)
		if err := img.Read(&buf); err != nil {
			return nil, err
		}
		out := make([]float64, n)
		for i, v := range buf {
			out[i] = float64(v)
		}
		return out, nil
	case 64:
		buf := make([]int64, n)
		if err := img.Read(&buf); err != nil {
			return nil, err
		}
		out := make([]float64, n)
		for i, v := range buf {
			out[i] = float64(v)
		}
		return out, nil
	case -32:
		buf := make([]float32, n)
		if err := img.Read(&buf); err != nil {
			return nil, err
		}
		out := make([]float64, n)
		for i, v := range buf {
			out[i] = float64(v)
		}
		return out, nil
	case -64:
		buf := make([]float64, n)
		if err := img.Read(&buf); err != nil {
			return nil, err
		}
		return buf, nil
	default:
		return nil, fmt.Errorf("unsupported BITPIX=%d", bitpix)
	}
}
func ReadFirst2DImage(path string) (GrayImage, error) {
	fh, err := os.Open(path)
	if err != nil {
		return GrayImage{}, err
	}
	defer fh.Close()

	f, err := fitsio.Open(fh)
	if err != nil {
		return GrayImage{}, err
	}
	defer f.Close()

	for _, hdu := range f.HDUs() {
		img, ok := hdu.(fitsio.Image)
		if !ok {
			continue
		}

		axes := img.Header().Axes()
		if len(axes) != 2 {
			continue
		}
		if axes[0] <= 0 || axes[1] <= 0 {
			continue
		}

		w, h := axes[0], axes[1]
		n := w * h

		pix, err := readPixelsAsFloat64(img, n)
		if err != nil {
			return GrayImage{}, fmt.Errorf("%s: %w", path, err)
		}

		sortedVals := make([]float64, 0, len(pix))
		for _, v := range pix {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				sortedVals = append(sortedVals, v)
			}
		}
		if len(sortedVals) == 0 {
			return GrayImage{}, errors.New("no finite pixels")
		}
		sort.Float64s(sortedVals)

		return GrayImage{
			W:      w,
			H:      h,
			Pix:    pix,
			Sorted: sortedVals,
		}, nil
	}

	return GrayImage{}, errors.New("no 2D image HDU found")
}
func MustRenderAndSave(
	materialized materialize.Result,
	preset Preset,
	layout outputlayout.Layout,
) {
	redImg, err := ReadFirst2DImage(materialized.Files["red"])
	if err != nil {
		log.Fatalf("read red fits: %v", err)
	}

	greenImg, err := ReadFirst2DImage(materialized.Files["green"])
	if err != nil {
		log.Fatalf("read green fits: %v", err)
	}

	blueImg, err := ReadFirst2DImage(materialized.Files["blue"])
	if err != nil {
		log.Fatalf("read blue fits: %v", err)
	}

	if redImg.W != greenImg.W || redImg.H != greenImg.H || redImg.W != blueImg.W || redImg.H != blueImg.H {
		log.Fatalf(
			"shape mismatch: red=%dx%d green=%dx%d blue=%dx%d",
			redImg.W, redImg.H,
			greenImg.W, greenImg.H,
			blueImg.W, blueImg.H,
		)
	}

	if err := os.MkdirAll(layout.RenderDir, 0o755); err != nil {
		log.Fatalf("mkdir render dir: %v", err)
	}

	img, err := RenderPreset(redImg, greenImg, blueImg, preset)
	if err != nil {
		log.Fatalf("render preset: %v", err)
	}

	if err := SavePNG(layout.ImagePath, img); err != nil {
		log.Fatalf("save png: %v", err)
	}
}

func percentileFromSorted(sortedVals []float64, p float64) float64 {
	if len(sortedVals) == 0 {
		return 0
	}
	if p <= 0 {
		return sortedVals[0]
	}
	if p >= 100 {
		return sortedVals[len(sortedVals)-1]
	}

	pos := (p / 100.0) * float64(len(sortedVals)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sortedVals[lo]
	}

	t := pos - float64(lo)
	return sortedVals[lo]*(1-t) + sortedVals[hi]*t
}
func RenderPreset(red, green, blue GrayImage, p Preset) (*image.RGBA, error) {
	if red.W != green.W || red.W != blue.W || red.H != green.H || red.H != blue.H {
		return nil, errors.New("shape mismatch")
	}

	rLo := percentileFromSorted(red.Sorted, p.RLow)
	rHi := percentileFromSorted(red.Sorted, p.RHigh)
	gLo := percentileFromSorted(green.Sorted, p.GLow)
	gHi := percentileFromSorted(green.Sorted, p.GHigh)
	bLo := percentileFromSorted(blue.Sorted, p.BLow)
	bHi := percentileFromSorted(blue.Sorted, p.BHigh)

	if rHi <= rLo {
		rHi = rLo + 1e-9
	}
	if gHi <= gLo {
		gHi = gLo + 1e-9
	}
	if bHi <= bLo {
		bHi = bLo + 1e-9
	}

	dst := image.NewRGBA(image.Rect(0, 0, red.W, red.H))
	for y := 0; y < red.H; y++ {
		for x := 0; x < red.W; x++ {
			i := y*red.W + x

			r := normalize(red.Pix[i], rLo, rHi)
			g := normalize(green.Pix[i], gLo, gHi)
			b := normalize(blue.Pix[i], bLo, bHi)

			r = applyStretch(r, p.Mode) * p.RGain
			g = applyStretch(g, p.Mode) * p.GGain
			b = applyStretch(b, p.Mode) * p.BGain

			dst.SetRGBA(x, y, color.RGBA{
				R: toByte(r),
				G: toByte(g),
				B: toByte(b),
				A: 255,
			})
		}
	}
	return dst, nil
}

func normalize(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return clamp01((v - lo) / (hi - lo))
}

func applyStretch(x float64, mode string) float64 {
	x = clamp01(x)
	switch mode {
	case "linear":
		return x
	case "sqrt":
		return math.Sqrt(x)
	case "log":
		return math.Log1p(1000*x) / math.Log1p(1000)
	case "asinh":
		return math.Asinh(10*x) / math.Asinh(10)
	default:
		return x
	}
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func toByte(x float64) uint8 {
	x = clamp01(x)
	return uint8(math.Round(x * 255))
}

func SavePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
