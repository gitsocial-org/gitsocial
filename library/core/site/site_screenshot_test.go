// site_screenshot_test.go - the pixel comparer the screenshot goldens are checked with

package site

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// channelTolerance is the per-channel difference a pixel may carry and still count as equal.
const channelTolerance = 8

// diffFraction is the share of differing pixels a screenshot may carry and still match its golden.
const diffFraction = 0.001

// diffResult reports how far a screenshot is from its golden.
type diffResult struct {
	differing int
	total     int
	fraction  float64
	image     image.Image
}

// readPNG decodes one PNG file.
func readPNG(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return img, nil
}

// writePNG encodes one image to a file.
func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// beyondTolerance reports whether two pixels differ on any channel past the tolerance.
func beyondTolerance(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	for _, pair := range [4][2]uint32{{ar, br}, {ag, bg}, {ab, bb}, {aa, ba}} {
		d := int(pair[0]>>8) - int(pair[1]>>8)
		if d < 0 {
			d = -d
		}
		if d > channelTolerance {
			return true
		}
	}
	return false
}

// comparePNGs counts the pixels two images differ on and paints a red diff image.
func comparePNGs(got, want image.Image) diffResult {
	gb, wb := got.Bounds(), want.Bounds()
	width, height := max(gb.Dx(), wb.Dx()), max(gb.Dy(), wb.Dy())
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	res := diffResult{total: width * height, image: out}
	for y := range height {
		for x := range width {
			g := pixelAt(got, gb, x, y)
			w := pixelAt(want, wb, x, y)
			if beyondTolerance(g, w) {
				res.differing++
				out.Set(x, y, color.RGBA{R: 255, A: 255})
				continue
			}
			gr, gg, gbl, _ := g.RGBA()
			out.Set(x, y, color.RGBA{R: uint8(gr >> 10), G: uint8(gg >> 10), B: uint8(gbl >> 10), A: 255})
		}
	}
	if res.total > 0 {
		res.fraction = float64(res.differing) / float64(res.total)
	}
	return res
}

// pixelAt reads a pixel, treating anything outside the image as transparent.
func pixelAt(img image.Image, b image.Rectangle, x, y int) color.Color {
	p := image.Pt(b.Min.X+x, b.Min.Y+y)
	if !p.In(b) {
		return color.RGBA{}
	}
	return img.At(p.X, p.Y)
}

// TestComparePNGs_tolerances holds the comparer's two thresholds over synthetic images.
func TestComparePNGs_tolerances(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			base.Set(x, y, color.RGBA{R: 100, G: 100, B: 100, A: 255})
		}
	}
	same := comparePNGs(base, base)
	if same.differing != 0 {
		t.Errorf("identical images differ on %d pixels", same.differing)
	}

	nudged := image.NewRGBA(base.Bounds())
	copy(nudged.Pix, base.Pix)
	for y := range 100 {
		for x := range 100 {
			nudged.Set(x, y, color.RGBA{R: 100 + channelTolerance, G: 100, B: 100, A: 255})
		}
	}
	if within := comparePNGs(nudged, base); within.differing != 0 {
		t.Errorf("a shift of %d counts as differing on %d pixels", channelTolerance, within.differing)
	}

	shifted := image.NewRGBA(base.Bounds())
	copy(shifted.Pix, base.Pix)
	for x := range 100 {
		shifted.Set(x, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	}
	beyond := comparePNGs(shifted, base)
	if beyond.differing != 100 {
		t.Errorf("one changed row differs on %d pixels, want 100", beyond.differing)
	}
	if beyond.fraction <= diffFraction {
		t.Errorf("one changed row is %.4f of the image, inside the %.4f threshold", beyond.fraction, diffFraction)
	}

	taller := image.NewRGBA(image.Rect(0, 0, 100, 101))
	if grown := comparePNGs(taller, base); grown.differing == 0 {
		t.Error("a different size compares as equal")
	}

	path := filepath.Join(t.TempDir(), "diff.png")
	if err := writePNG(path, beyond.image); err != nil {
		t.Fatalf("write diff: %v", err)
	}
	back, err := readPNG(path)
	if err != nil {
		t.Fatalf("read diff: %v", err)
	}
	if reread := comparePNGs(back, beyond.image); reread.differing != 0 {
		t.Errorf("a written diff reads back differing on %d pixels", reread.differing)
	}
}
