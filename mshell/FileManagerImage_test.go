package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestParseSixelRepliesWindowsTerminal(t *testing.T) {
	reply := "\x1b[6;20;10t\x1b[4;1340;1550t\x1b[?61;4;6;7;14;21;22;23;24;28;32;42;52c"
	got := parseSixelReplies(reply, 155, 67)
	want := sixelTerminal{supported: true, cellW: 10, cellH: 20}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseSixelRepliesCellSizeFromTextArea(t *testing.T) {
	got := parseSixelReplies("\x1b[4;400;800t\x1b[?62;4c", 80, 20)
	want := sixelTerminal{supported: true, cellW: 10, cellH: 20}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseSixelRepliesWithoutSixel(t *testing.T) {
	// Attribute 42 contains a 4 but is not attribute 4.
	got := parseSixelReplies("\x1b[6;20;10t\x1b[?62;22;42c", 80, 20)
	if got.supported {
		t.Fatalf("got sixel support from %+v", got)
	}
}

func TestHasDA1Reply(t *testing.T) {
	for _, tc := range []struct {
		reply string
		want  bool
	}{
		{"", false},
		{"\x1b[6;20;10t", false},
		{"\x1b[6;20;10t\x1b[?61;4", false},
		{"\x1b[6;20;10t\x1b[?61;4c", true},
	} {
		if got := hasDA1Reply([]byte(tc.reply)); got != tc.want {
			t.Errorf("hasDA1Reply(%q) = %v, want %v", tc.reply, got, tc.want)
		}
	}
}

func TestFitImageKeepsAspectRatio(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for _, tc := range []struct {
		maxW, maxH int
		wantW      int
		wantH      int
	}{
		{100, 100, 100, 50},
		{800, 50, 100, 50},
		{1000, 1000, 400, 200}, // never enlarged
	} {
		got := fitImage(src, tc.maxW, tc.maxH, 1_000_000, 1).Bounds()
		if got.Dx() != tc.wantW || got.Dy() != tc.wantH {
			t.Errorf("fit in %dx%d = %dx%d, want %dx%d", tc.maxW, tc.maxH, got.Dx(), got.Dy(), tc.wantW, tc.wantH)
		}
	}
}

func TestFitImageAveragesPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{200, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{0, 100, 0, 255})
	got := fitImage(src, 1, 1, 1, 1).RGBAAt(0, 0)
	if want := (color.RGBA{100, 50, 0, 255}); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestQuantizeImageKeepsFewColorsExact(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 1))
	img.SetRGBA(0, 0, color.RGBA{12, 34, 56, 255})
	img.SetRGBA(1, 0, color.RGBA{200, 100, 50, 255})
	// Pixel 2 is left transparent.
	got := quantizeImage(img)
	if len(got.Palette) != 3 {
		t.Fatalf("palette has %d colors, want 3", len(got.Palette))
	}
	if got.Pix[2] != 0 {
		t.Fatalf("transparent pixel has index %d, want 0", got.Pix[2])
	}
	for x, want := range []color.RGBA{{12, 34, 56, 255}, {200, 100, 50, 255}} {
		if c := got.Palette[got.Pix[x]]; c != want {
			t.Errorf("pixel %d = %v, want %v", x, c, want)
		}
	}
}

func TestQuantizeImageDithersPhotos(t *testing.T) {
	// A smooth gradient with no dominant color, like a photo.
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for y := 0; y < 128; y++ {
		for x := 0; x < 128; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 2), uint8(y * 2), uint8(x + y), 255})
		}
	}
	img.SetRGBA(5, 5, color.RGBA{})
	got := quantizeImage(img)
	if len(got.Palette) > 256 {
		t.Fatalf("palette has %d colors", len(got.Palette))
	}
	for i, p := range got.Pix {
		if (p == 0) != (i == 5*128+5) {
			t.Fatalf("pixel %d has index %d; only pixel %d is transparent", i, p, 5*128+5)
		}
	}
	// The average color of a region stays close to the original.
	var want, have [3]int
	for y := 40; y < 60; y++ {
		for x := 40; x < 60; x++ {
			o := img.RGBAAt(x, y)
			q := got.Palette[got.ColorIndexAt(x, y)].(color.RGBA)
			want[0], want[1], want[2] = want[0]+int(o.R), want[1]+int(o.G), want[2]+int(o.B)
			have[0], have[1], have[2] = have[0]+int(q.R), have[1]+int(q.G), have[2]+int(q.B)
		}
	}
	for k := 0; k < 3; k++ {
		if d := (want[k] - have[k]) / 400; d < -2 || d > 2 {
			t.Errorf("channel %d averages %d, want %d", k, have[k]/400, want[k]/400)
		}
	}
}

func TestQuantizeImageKeepsFlatAreasFlat(t *testing.T) {
	// A screenshot: a flat background with text drawn in many shades.
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	background := color.RGBA{34, 34, 34, 255}
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			img.SetRGBA(x, y, background)
		}
	}
	for i := 0; i < 1000; i++ {
		x, y := 50+i%100, 40+i/100
		img.SetRGBA(x, y, color.RGBA{uint8(i % 256), uint8(i / 4 % 256), 200, 255})
	}
	got := quantizeImage(img)
	if len(got.Palette) > 256 {
		t.Fatalf("palette has %d colors", len(got.Palette))
	}
	// Every background pixel, including those next to the text, is the
	// exact background color.
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			if img.RGBAAt(x, y) != background {
				continue
			}
			if c := got.Palette[got.ColorIndexAt(x, y)]; c != background {
				t.Fatalf("background pixel %d,%d is %v", x, y, c)
			}
		}
	}
}

func TestQuantizeImageManyExactColors(t *testing.T) {
	// More distinct colors than are counted exactly, all equally common.
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for i := 0; i < 10000; i++ {
		img.SetRGBA(i%100, i/100, color.RGBA{uint8(i), uint8(i >> 8), uint8(i * 7), 255})
	}
	if got := quantizeImage(img); len(got.Palette) > 256 {
		t.Fatalf("palette has %d colors", len(got.Palette))
	}
}

func TestFitImageBlendsWhenWidening(t *testing.T) {
	// Two pixels widened to four: each source pixel covers two output
	// pixels exactly.
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})
	src.SetRGBA(1, 0, color.RGBA{200, 200, 200, 255})
	got := fitImage(src, 4, 1, 100, 0.5)
	want := []uint8{0, 0, 200, 200}
	for x, v := range want {
		if c := got.RGBAAt(x, 0); c.R != v {
			t.Errorf("pixel %d = %v, want red %d", x, c, v)
		}
	}
	// Two pixels widened to three: the middle one is half of each.
	got = fitImage(src, 3, 1, 100, 2.0/3)
	if c := got.RGBAAt(1, 0); c.R != 100 {
		t.Errorf("middle pixel = %v, want red 100", c)
	}
}

func TestSourceSpansWeightsAddUp(t *testing.T) {
	for _, tc := range [][2]int{{10, 3}, {3, 10}, {7, 7}, {1, 5}, {5, 1}, {1459, 1824}} {
		for i, spans := range sourceSpans(tc[0], tc[1]) {
			total := 0.0
			for _, s := range spans {
				total += s.weight
			}
			if math.Abs(total-1) > 1e-9 {
				t.Fatalf("%d over %d: output %d weights add to %v", tc[0], tc[1], i, total)
			}
		}
	}
}

// decodeTestSixel reads back what encodeSixel writes into palette indexes.
func decodeTestSixel(t *testing.T, data []byte) (int, int, []uint8) {
	t.Helper()
	m := regexp.MustCompile(`^\x1bP0;1;0q"1;1;(\d+);(\d+)`).FindSubmatch(data)
	if m == nil || !bytes.HasSuffix(data, []byte("\x1b\\")) {
		t.Fatalf("sixel data has no expected start and end: %q", data)
	}
	w, _ := strconv.Atoi(string(m[1]))
	h, _ := strconv.Atoi(string(m[2]))
	body := data[len(m[0]) : len(data)-2]
	pix := make([]uint8, w*h)
	x, top, color := 0, 0, 0
	number := func(i int) (int, int) {
		n := 0
		for i < len(body) && body[i] >= '0' && body[i] <= '9' {
			n = n*10 + int(body[i]-'0')
			i++
		}
		return n, i
	}
	for i := 0; i < len(body); {
		c := body[i]
		switch {
		case c == '#':
			n, next := number(i + 1)
			i = next
			if i < len(body) && body[i] == ';' {
				// A color definition.
				for i < len(body) && (body[i] == ';' || (body[i] >= '0' && body[i] <= '9')) {
					i++
				}
			} else {
				color = n
			}
		case c == '$':
			x = 0
			i++
		case c == '-':
			x = 0
			top += 6
			i++
		case c == '!' || (c >= '?' && c <= '~'):
			count := 1
			if c == '!' {
				count, i = number(i + 1)
				c = body[i]
			}
			i++
			for k := 0; k < count; k++ {
				for dy := 0; dy < 6; dy++ {
					if (c-'?')&(1<<dy) == 0 {
						continue
					}
					if x >= w || top+dy >= h {
						t.Fatalf("pixel %d,%d is outside the %dx%d image", x, top+dy, w, h)
					}
					pix[(top+dy)*w+x] = uint8(color)
				}
				x++
			}
		default:
			t.Fatalf("unexpected byte %q at %d", c, i)
		}
	}
	return w, h, pix
}

func TestEncodeSixelRoundTrip(t *testing.T) {
	// 7 rows high so the last band is partly filled; 40 wide so repeats
	// are written as counts.
	img := image.NewRGBA(image.Rect(0, 0, 40, 7))
	for y := 0; y < 7; y++ {
		for x := 0; x < 40; x++ {
			if x == 3 && y == 2 {
				continue // transparent
			}
			img.SetRGBA(x, y, color.RGBA{uint8(x / 10 * 60), uint8(y * 30), 0, 255})
		}
	}
	paletted := quantizeImage(img)
	w, h, pix := decodeTestSixel(t, encodeSixel(paletted, maxSixelBytes))
	if w != 40 || h != 7 {
		t.Fatalf("size %dx%d, want 40x7", w, h)
	}
	if !bytes.Equal(pix, paletted.Pix) {
		t.Fatalf("decoded pixels differ from the encoded image")
	}
}

func TestEncodeSixelColorDefinitions(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.RGBA{}, color.RGBA{255, 128, 0, 255}})
	img.Pix[0] = 1
	if got := string(encodeSixel(img, maxSixelBytes)); !bytes.Contains([]byte(got), []byte("#1;2;100;50;0")) {
		t.Fatalf("missing color definition in %q", got)
	}
}

func writeTestPNG(t *testing.T, w int, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	path := filepath.Join(t.TempDir(), "picture.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPreviewImage(t *testing.T) {
	path := writeTestPNG(t, 200, 100)

	lines, sixel := previewImage(path, 50, 50, 1)
	if len(lines) != 1 || lines[0] != " PNG image, 200x100" {
		t.Fatalf("lines = %q", lines)
	}
	w, h, _ := decodeTestSixel(t, sixel)
	if w != 50 || h != 25 {
		t.Fatalf("image is %dx%d, want 50x25", w, h)
	}

	lines, sixel = previewImage(path, 0, 0, 1)
	if sixel != nil || len(lines) != 1 {
		t.Fatalf("got lines %q and %d bytes of sixel without a pixel size", lines, len(sixel))
	}
}

func TestComputePreviewDescribesImage(t *testing.T) {
	path := writeTestPNG(t, 3, 2)
	lines := computePreview(testDirEntry{name: "picture.png"}, path, 10, false)
	if len(lines) != 1 || lines[0] != " PNG image, 3x2" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestSchedulePreviewRequestsImageSize(t *testing.T) {
	dir := t.TempDir()
	fm := &FileManager{
		rows:          23,
		cols:          100,
		currentDir:    dir,
		entries:       []os.DirEntry{testDirEntry{name: "a.png"}},
		previewCache:  map[string][]string{},
		previewImages: map[string]imagePreview{},
		previewReqCh:  make(chan previewRequest, 1),
		sixel:         sixelTerminal{supported: true, cellW: 10, cellH: 20},
	}
	wantW := (fm.cols - fm.leftPaneWidth() - 4) * 10
	wantH := (fm.rows - imagePreviewRow) * 20

	fm.schedulePreview()
	req := <-fm.previewReqCh
	if req.imageW != wantW || req.imageH != wantH {
		t.Fatalf("image size %dx%d, want %dx%d", req.imageW, req.imageH, wantW, wantH)
	}

	// An image made for another pane width is made again.
	path := filepath.Join(dir, "a.png")
	fm.previewCache[path] = []string{" PNG image, 1x1"}
	fm.previewImages[path] = imagePreview{sixel: []byte("x"), imageW: wantW + 10}
	if fm.selectedImage() != nil {
		t.Fatal("drew an image made for another pane width")
	}
	fm.schedulePreview()
	select {
	case <-fm.previewReqCh:
	default:
		t.Fatal("no new request for an image made for another pane width")
	}

	// One made for this width is kept.
	fm.previewCache[path] = []string{" PNG image, 1x1"}
	fm.previewImages[path] = imagePreview{sixel: []byte("x"), imageW: wantW}
	fm.schedulePreview()
	select {
	case req := <-fm.previewReqCh:
		t.Fatalf("unexpected request %+v", req)
	default:
	}
	if got := fm.selectedImage(); string(got) != "x" {
		t.Fatalf("selectedImage = %q", got)
	}
}

func BenchmarkPreviewImage(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1600, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 1600; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	path := filepath.Join(b.TempDir(), "picture.png")
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, sixel := previewImage(path, 750, 1280, 1)
		if i == 0 {
			b.ReportMetric(float64(len(sixel)), "bytes")
		}
	}
}

func TestParseCellPixels(t *testing.T) {
	for _, tc := range []struct {
		value string
		w, h  int
		ok    bool
	}{
		{"9x20", 9, 20, true},
		{" 10X21 ", 10, 21, true},
		{"1x1", 1, 1, true},
		{"1x4", 1, 4, true},
		{"256x256", 256, 256, true},
		{"5x20", 5, 20, true},
		{"4x20", 0, 0, false}, // more than 4 times as tall as wide
		{"20x4", 0, 0, false},
		{"", 0, 0, false},
		{"9", 0, 0, false},
		{"9x", 0, 0, false},
		{"x20", 0, 0, false},
		{"0x20", 0, 0, false},
		{"9x257", 0, 0, false},
		{"-9x20", 0, 0, false},
		{"9.5x20", 0, 0, false},
		{"9x20x3", 0, 0, false},
		{"99999999999999999999x20", 0, 0, false},
	} {
		w, h, err := parseCellPixels(tc.value)
		if (err == nil) != tc.ok || w != tc.w || h != tc.h {
			t.Errorf("parseCellPixels(%q) = %d, %d, %v", tc.value, w, h, err)
		}
	}
}

func TestApplyCellPixelsSetting(t *testing.T) {
	reported := sixelTerminal{supported: true, cellW: 10, cellH: 20}

	// The reported size is kept; the setting gives the real cell shape.
	got, message := applyCellPixelsSetting(reported, "9x20", true)
	if got != (sixelTerminal{supported: true, cellW: 10, cellH: 20, pixelAspect: 0.9}) || message != "" {
		t.Fatalf("valid setting: got %+v, %q", got, message)
	}

	// Same shape as reported: square pixels.
	got, _ = applyCellPixelsSetting(reported, "12x24", true)
	if got.pixelAspect != 1 {
		t.Fatalf("got %+v", got)
	}

	// A bad setting is reported and ignored.
	got, message = applyCellPixelsSetting(reported, "wide", true)
	if got != (sixelTerminal{supported: true, cellW: 10, cellH: 20, pixelAspect: 1}) || !strings.Contains(message, cellPixelsEnvVar) {
		t.Fatalf("bad setting: got %+v, %q", got, message)
	}

	// The setting gives a size to a terminal that reported none.
	got, _ = applyCellPixelsSetting(sixelTerminal{supported: true}, "8x16", true)
	if got != (sixelTerminal{supported: true, cellW: 8, cellH: 16, pixelAspect: 1}) {
		t.Fatalf("got %+v", got)
	}

	// With no size at all, images are off.
	got, _ = applyCellPixelsSetting(sixelTerminal{supported: true}, "", false)
	if got.supported {
		t.Fatal("images on without a cell size")
	}

	// The setting does not turn on sixel for a terminal without it.
	got, _ = applyCellPixelsSetting(sixelTerminal{}, "9x20", true)
	if got.supported {
		t.Fatal("images on for a terminal without sixel")
	}
}

func TestFitImageMakesUpForNarrowPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	// Pixels that appear 0.8 as wide as tall: the image is drawn 1.25 times
	// wider, so it appears 2:1 on screen.
	got := fitImage(src, 1000, 1000, 1_000_000, 0.8).Bounds()
	if got.Dx() != 500 || got.Dy() != 200 {
		t.Fatalf("got %dx%d, want 500x200", got.Dx(), got.Dy())
	}
	// The box still limits the size.
	got = fitImage(src, 250, 1000, 1_000_000, 0.8).Bounds()
	if got.Dx() != 250 || got.Dy() != 100 {
		t.Fatalf("got %dx%d, want 250x100", got.Dx(), got.Dy())
	}
	// Bad values are treated as square pixels.
	for _, aspect := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		got = fitImage(src, 1000, 1000, 1_000_000, aspect).Bounds()
		if got.Dx() != 400 || got.Dy() != 200 {
			t.Fatalf("aspect %v: got %dx%d, want 400x200", aspect, got.Dx(), got.Dy())
		}
	}
}

func TestParseSixelRepliesIgnoresBadCellSizes(t *testing.T) {
	for _, reply := range []string{
		"\x1b[6;0;10t\x1b[?62;4c",
		"\x1b[6;20;9999t\x1b[?62;4c",
		"\x1b[4;99999999;99999999t\x1b[?62;4c",
		"\x1b[6;99999999999999999999;10t\x1b[?62;4c",
	} {
		got := parseSixelReplies(reply, 80, 24)
		if got.cellW != 0 || got.cellH != 0 {
			t.Errorf("parseSixelReplies(%q) = %+v, want no cell size", reply, got)
		}
	}
}

func TestFitImageLimitsTotalPixels(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 200))
	got := fitImage(src, 400, 200, 20_000, 1).Bounds()
	if got.Dx()*got.Dy() > 20_000 || got.Dx() != 2*got.Dy() {
		t.Fatalf("got %dx%d, want at most 20000 pixels at 2:1", got.Dx(), got.Dy())
	}
}

func TestEncodeSixelSizeLimit(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 4), uint8(y * 4), uint8(x ^ y), 255})
		}
	}
	paletted := quantizeImage(img)
	full := encodeSixel(paletted, maxSixelBytes)
	if full == nil {
		t.Fatal("no data within the normal limit")
	}
	if got := encodeSixel(paletted, len(full)-1); got != nil {
		t.Fatalf("got %d bytes with a limit of %d", len(got), len(full)-1)
	}
	if got := encodeSixel(paletted, len(full)); !bytes.Equal(got, full) {
		t.Fatal("data at exactly the limit was refused")
	}
}

// writePNGHeader writes the start of a PNG that says it is w x h pixels,
// without the pixel data.
func writePNGHeader(t *testing.T, w uint32, h uint32) string {
	t.Helper()
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 6 // RGBA
	var data bytes.Buffer
	data.WriteString("\x89PNG\r\n\x1a\n")
	binary.Write(&data, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	data.Write(chunk)
	binary.Write(&data, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	path := filepath.Join(t.TempDir(), "huge.png")
	if err := os.WriteFile(path, data.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPreviewImageRefusesHugeImage(t *testing.T) {
	path := writePNGHeader(t, 100_000, 100_000)
	lines, sixel := previewImage(path, 500, 500, 1)
	if sixel != nil || len(lines) != 2 || lines[0] != " PNG image, 100000x100000" || lines[1] != " (too large to preview)" {
		t.Fatalf("got lines %q and %d bytes of sixel", lines, len(sixel))
	}
}

func TestPreviewImageRefusesLargeFile(t *testing.T) {
	path := writeTestPNG(t, 4, 4)
	// Grow the file past the limit; the PNG decoder ignores the trailing data.
	if err := os.Truncate(path, maxImageFileBytes+1); err != nil {
		t.Skip("cannot make a large sparse file:", err)
	}
	lines, sixel := previewImage(path, 500, 500, 1)
	if sixel != nil || len(lines) != 2 || lines[1] != " (too large to preview)" {
		t.Fatalf("got lines %q and %d bytes of sixel", lines, len(sixel))
	}
}

func TestPreviewImageSkipsNonRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "folder.png")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	lines, sixel := previewImage(path, 500, 500, 1)
	if sixel != nil || len(lines) != 1 || lines[0] != " (not a regular file)" {
		t.Fatalf("got lines %q and %d bytes of sixel", lines, len(sixel))
	}
}

func TestComputePreviewReadsLimitedText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(path, bytes.Repeat([]byte("a"), maxTextPreviewBytes*2), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := computePreview(testDirEntry{name: "long.txt"}, path, 10, false)
	if len(lines) != 1 || len(lines[0]) != maxTextPreviewBytes+1 {
		t.Fatalf("got %d lines, first %d bytes", len(lines), len(lines[0]))
	}
}

func TestImagePreviewCacheDropsOldest(t *testing.T) {
	fm := &FileManager{previewCache: map[string][]string{}}
	size := maxImageCacheBytes / 3
	for _, name := range []string{"a", "b", "c", "d"} {
		fm.previewCache[name] = []string{name}
		fm.cacheImagePreview(name, imagePreview{sixel: make([]byte, size), imageW: 1})
	}
	if _, ok := fm.previewImages["a"]; ok {
		t.Fatal("oldest image kept past the cache limit")
	}
	if _, ok := fm.previewCache["a"]; ok {
		t.Fatal("text kept for a dropped image, so it would not be made again")
	}
	for _, name := range []string{"b", "c", "d"} {
		if _, ok := fm.previewImages[name]; !ok {
			t.Errorf("image %s dropped", name)
		}
	}
	if fm.previewImageBytes != 3*size {
		t.Fatalf("cache counts %d bytes, want %d", fm.previewImageBytes, 3*size)
	}

	// Replacing an image counts only the new one.
	fm.cacheImagePreview("d", imagePreview{sixel: make([]byte, 10), imageW: 2})
	if fm.previewImageBytes != 2*size+10 {
		t.Fatalf("cache counts %d bytes, want %d", fm.previewImageBytes, 2*size+10)
	}
}

func TestImagePreviewSizeIsCapped(t *testing.T) {
	fm := &FileManager{rows: 2000, cols: 3000, sixel: sixelTerminal{supported: true, cellW: 256, cellH: 256}}
	w, h := fm.imagePreviewSize()
	if w != maxImageSide || h != maxImageSide {
		t.Fatalf("got %dx%d, want %dx%d", w, h, maxImageSide, maxImageSide)
	}
}
