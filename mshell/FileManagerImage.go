package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Image previews in the file manager are drawn as sixel graphics, a format
// that terminals such as Windows Terminal, WezTerm, foot, and xterm (as a
// VT340) can show. They are only drawn after the terminal says it supports
// sixel and the size of one text cell in pixels is known.

// sixelTerminal holds what is known about the terminal's sixel support.
type sixelTerminal struct {
	supported bool
	// Image pixels per text cell, as the terminal reports them. The
	// terminal maps this many image pixels onto each cell.
	cellW int
	cellH int
	// Width over height of one image pixel as it appears on screen. It is
	// 1 unless MSH_CELL_PIXELS says the real cells have a different shape
	// than the reported ones. Zero also means 1.
	pixelAspect float64
}

// cellPixelsEnvVar names the environment variable that gives the real size
// of a text cell in pixels, such as "9x20". Windows Terminal reports 10x20
// cells whatever the font and stretches images onto the real cells, so with
// a font whose cells are not 1:2 images come out squeezed. With the real
// size, images are drawn stretched the other way to make up for it.
const cellPixelsEnvVar = "MSH_CELL_PIXELS"

// Limits on each step of making an image preview. Previews run in the
// background of an interactive shell, so a bad file or terminal must not be
// able to use up memory or stall it.
const (
	// terminalQueryTimeout bounds how long startup waits for the terminal to
	// answer. Every terminal in use answers DA1, so the wait normally ends as
	// soon as the reply arrives.
	terminalQueryTimeout = time.Second
	// maxTerminalReplyBytes stops reading replies from a terminal that keeps
	// sending without ever answering DA1.
	maxTerminalReplyBytes = 4096
	// maxCellPixels is the largest cell width or height accepted, from the
	// terminal or from MSH_CELL_PIXELS.
	maxCellPixels = 256
	// maxCellShapeRatio is how many times one side of a cell given in
	// MSH_CELL_PIXELS may be the other.
	maxCellShapeRatio = 4
	// maxImageFileBytes is the most read from an image file.
	maxImageFileBytes = 256 << 20
	// maxImageSourcePixels skips decoding larger images. 50 million pixels
	// is 200 MB once decoded, or 400 MB for 16 bit PNGs.
	maxImageSourcePixels = 50_000_000
	// maxImageSide and maxImageOutputPixels bound the size of the drawn
	// image, whatever the terminal and cell sizes are.
	maxImageSide         = 4096
	maxImageOutputPixels = 8_000_000
	// maxSixelBytes bounds the encoded image. It is written to the terminal
	// on every redraw.
	maxSixelBytes = 8 << 20
	// maxImageCacheBytes bounds the encoded images kept in memory. The
	// oldest are dropped first.
	maxImageCacheBytes = 64 << 20
)

// detectSixel asks the terminal whether it supports sixel and for its cell
// size. The terminal must already be in raw mode. It writes the queries to
// out and reads the replies from stdin. The cell size is zero if the
// terminal gave none.
func detectSixel(out *os.File, inFd int, cols int, rows int) sixelTerminal {
	// Cell size in pixels, text area size in pixels, then primary device
	// attributes (DA1). DA1 goes last: terminals answer queries in order and
	// all of them answer DA1, so its reply means no more replies are coming.
	if _, err := out.WriteString("\033[16t\033[14t\033[c"); err != nil {
		return sixelTerminal{}
	}

	var reply []byte
	buf := make([]byte, 256)
	deadline := time.Now().Add(terminalQueryTimeout)
	for !hasDA1Reply(reply) && len(reply) < maxTerminalReplyBytes {
		remaining := time.Until(deadline)
		if remaining <= 0 || !waitForInput(inFd, remaining) {
			break
		}
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			break
		}
		reply = append(reply, buf[:n]...)
	}

	result := parseSixelReplies(string(reply), cols, rows)
	if result.supported && result.cellW == 0 {
		w, h := windowPixelCellSize(inFd, cols, rows)
		if validCellPixels(w, h) {
			result.cellW, result.cellH = w, h
		}
	}
	return result
}

// applyCellPixelsSetting applies the MSH_CELL_PIXELS setting, if set, and
// turns images off when no cell size is known. It returns a message for the
// user when the setting is not valid.
//
// The terminal's reported cell size is kept, since it is how the terminal
// maps image pixels onto cells. The setting only gives the real shape of a
// cell, to work out how squeezed each image pixel appears. When the terminal
// reported no size, the setting is used as the cell size.
func applyCellPixelsSetting(term sixelTerminal, value string, set bool) (sixelTerminal, string) {
	message := ""
	term.pixelAspect = 1
	if set {
		w, h, err := parseCellPixels(value)
		switch {
		case err != nil:
			message = err.Error()
		case term.cellW == 0 || term.cellH == 0:
			term.cellW, term.cellH = w, h
		default:
			term.pixelAspect = (float64(w) / float64(h)) / (float64(term.cellW) / float64(term.cellH))
		}
	}
	// Without a cell size, the image cannot be fit to the preview pane.
	if term.cellW == 0 || term.cellH == 0 {
		term.supported = false
	}
	return term, message
}

// parseCellPixels reads a cell size written as WIDTHxHEIGHT, such as "9x20".
func parseCellPixels(value string) (int, int, error) {
	invalid := fmt.Errorf("%s=%q is not valid; use WIDTHxHEIGHT such as 9x20 (1-%d each, at most %d:1)",
		cellPixelsEnvVar, value, maxCellPixels, maxCellShapeRatio)
	widthText, heightText, ok := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "x")
	if !ok {
		return 0, 0, invalid
	}
	w, errW := strconv.Atoi(widthText)
	h, errH := strconv.Atoi(heightText)
	if errW != nil || errH != nil || !validCellPixels(w, h) || w > maxCellShapeRatio*h || h > maxCellShapeRatio*w {
		return 0, 0, invalid
	}
	return w, h, nil
}

func validCellPixels(w int, h int) bool {
	return w >= 1 && w <= maxCellPixels && h >= 1 && h <= maxCellPixels
}

// parseSixelReplies reads the terminal's replies to the queries sent by
// detectSixel. Cell sizes outside 1 to maxCellPixels are ignored.
func parseSixelReplies(reply string, cols int, rows int) sixelTerminal {
	var result sixelTerminal
	var areaW, areaH int
	for _, seq := range splitCSISequences(reply) {
		params, final := seq[:len(seq)-1], seq[len(seq)-1]
		switch {
		case final == 'c' && strings.HasPrefix(params, "?"):
			// Attribute 4 means sixel graphics.
			for _, p := range strings.Split(params[1:], ";") {
				if p == "4" {
					result.supported = true
				}
			}
		case final == 't':
			fields := strings.Split(params, ";")
			if len(fields) != 3 {
				continue
			}
			h, errH := strconv.Atoi(fields[1])
			w, errW := strconv.Atoi(fields[2])
			if errH != nil || errW != nil {
				continue
			}
			switch fields[0] {
			case "6":
				if validCellPixels(w, h) {
					result.cellW, result.cellH = w, h
				}
			case "4":
				areaW, areaH = w, h
			}
		}
	}
	if result.cellW == 0 && cols > 0 && rows > 0 && validCellPixels(areaW/cols, areaH/rows) {
		result.cellW, result.cellH = areaW/cols, areaH/rows
	}
	return result
}

func hasDA1Reply(reply []byte) bool {
	for _, seq := range splitCSISequences(string(reply)) {
		if seq[len(seq)-1] == 'c' && seq[0] == '?' {
			return true
		}
	}
	return false
}

// splitCSISequences returns each complete CSI sequence in s, without the
// leading ESC [.
func splitCSISequences(s string) []string {
	var seqs []string
	for {
		start := strings.Index(s, "\033[")
		if start < 0 {
			return seqs
		}
		s = s[start+2:]
		end := 0
		for end < len(s) && (s[end] < 0x40 || s[end] > 0x7e) {
			end++
		}
		if end == len(s) {
			return seqs
		}
		seqs = append(seqs, s[:end+1])
		s = s[end+1:]
	}
}

func isImagePreviewPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// previewImage returns the preview text for an image file and, when maxW and
// maxH are positive, the image scaled to fit in that many pixels as sixel data.
// pixelAspect is the width over height of one image pixel on screen; the
// image is drawn wider or taller to make up for pixels that are not square.
func previewImage(path string, maxW int, maxH int, pixelAspect float64) (lines []string, sixel []byte) {
	// The image decoders are not expected to panic, but a panic here would
	// end the whole shell, so a bad file only loses its preview.
	defer func() {
		if recover() != nil {
			lines, sixel = []string{" (cannot read image)"}, nil
		}
	}()

	// Only regular files: a named pipe or device named like an image could
	// block or never end.
	stat, err := os.Stat(path)
	if err != nil {
		return []string{" (cannot open for reading)"}, nil
	}
	if !stat.Mode().IsRegular() {
		return []string{" (not a regular file)"}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return []string{" (cannot open for reading)"}, nil
	}
	defer f.Close()

	config, format, err := image.DecodeConfig(io.LimitReader(f, maxImageFileBytes))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return []string{" (cannot read image)"}, nil
	}
	info := fmt.Sprintf(" %s image, %dx%d", strings.ToUpper(format), config.Width, config.Height)
	if maxW <= 0 || maxH <= 0 {
		return []string{info}, nil
	}
	if stat.Size() > maxImageFileBytes || int64(config.Width)*int64(config.Height) > maxImageSourcePixels {
		return []string{info, " (too large to preview)"}, nil
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return []string{info, " (cannot read image)"}, nil
	}
	img, _, err := image.Decode(io.LimitReader(f, maxImageFileBytes))
	if err != nil {
		return []string{info, " (cannot read image)"}, nil
	}
	fitted := fitImage(img, min(maxW, maxImageSide), min(maxH, maxImageSide), maxImageOutputPixels, pixelAspect)
	sixel = encodeSixel(quantizeImage(fitted), maxSixelBytes)
	if sixel == nil {
		return []string{info, " (too detailed to preview)"}, nil
	}
	return []string{info}, sixel
}

// fitImage scales img to fit in maxW x maxH pixels and maxPixels pixels in
// total, keeping its aspect ratio as it will appear on screen: with
// pixelAspect below 1, pixels appear narrower than tall, so the image is made
// that much wider. It does not enlarge beyond that. Each output pixel is the
// average of the source area it covers, with partly covered source pixels
// counted by how much of them is covered. The result has premultiplied alpha.
func fitImage(img image.Image, maxW int, maxH int, maxPixels int, pixelAspect float64) *image.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if pixelAspect <= 0 || math.IsNaN(pixelAspect) || math.IsInf(pixelAspect, 0) {
		pixelAspect = 1
	}
	fw, fh := float64(sw)/pixelAspect, float64(sh)
	if fw > float64(maxW) {
		fh = fh * float64(maxW) / fw
		fw = float64(maxW)
	}
	if fh > float64(maxH) {
		fw = fw * float64(maxH) / fh
		fh = float64(maxH)
	}
	if fw*fh > float64(maxPixels) {
		scale := math.Sqrt(float64(maxPixels) / (fw * fh))
		fw *= scale
		fh *= scale
	}
	w := min(max(int(fw+0.5), 1), maxW)
	h := min(max(int(fh+0.5), 1), maxH)

	cols := sourceSpans(sw, w)
	rows := sourceSpans(sh, h)

	// Source rows are converted one at a time, so no full size copy of the
	// image is made. A row on the border of two output rows is used by both,
	// so the last one converted is kept.
	row := image.NewRGBA(image.Rect(0, 0, sw, 1))
	rowIndex := -1
	sums := make([]float64, w*4)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		clear(sums)
		for _, rowSpan := range rows[y] {
			if rowSpan.index != rowIndex {
				draw.Draw(row, row.Bounds(), img, image.Pt(b.Min.X, b.Min.Y+rowSpan.index), draw.Src)
				rowIndex = rowSpan.index
			}
			for x := 0; x < w; x++ {
				for _, colSpan := range cols[x] {
					weight := rowSpan.weight * colSpan.weight
					p := row.Pix[colSpan.index*4 : colSpan.index*4+4]
					sums[x*4] += weight * float64(p[0])
					sums[x*4+1] += weight * float64(p[1])
					sums[x*4+2] += weight * float64(p[2])
					sums[x*4+3] += weight * float64(p[3])
				}
			}
		}
		out := dst.Pix[y*dst.Stride:]
		for i := 0; i < w*4; i++ {
			out[i] = uint8(min(max(sums[i]+0.5, 0), 255))
		}
	}
	return dst
}

// sourceSpan is one source pixel's share of an output pixel.
type sourceSpan struct {
	index  int
	weight float64 // fraction of the output pixel it covers
}

// sourceSpans splits n source pixels evenly over m output pixels and returns,
// for each output pixel, the source pixels it covers and how much of it each
// one covers. The weights of each output pixel add up to 1.
func sourceSpans(n int, m int) [][]sourceSpan {
	spans := make([][]sourceSpan, m)
	scale := float64(n) / float64(m)
	for i := 0; i < m; i++ {
		start := float64(i) * scale
		end := float64(i+1) * scale
		for j := int(start); j < n && float64(j) < end; j++ {
			covered := min(end, float64(j+1)) - max(start, float64(j))
			if covered > 0 {
				spans[i] = append(spans[i], sourceSpan{index: j, weight: covered / scale})
			}
		}
	}
	return spans
}

// Limits on choosing a palette for an image with more colors than a sixel
// palette holds.
const (
	// maxExactColorCount is how many distinct colors are counted exactly.
	// Colors seen after that are only counted in the coarse buckets.
	maxExactColorCount = 4096
	// maxKeptColors is how many of the most common colors can be kept
	// exactly, and keptColorShare is the share of the pixels a color must
	// cover to be kept: 1 in 500.
	maxKeptColors  = 128
	keptColorShare = 500
)

// quantizeImage maps img onto at most 255 colors for sixel output. Palette
// index 0 is kept for transparent pixels, which are not drawn.
//
// Images with 255 colors or fewer keep their exact colors. Otherwise the
// most common colors, such as the background of a screenshot, keep exact
// palette entries, and the rest of the palette is chosen from the image's
// other colors. When those common colors cover most of the image, as in
// screenshots and diagrams, each pixel takes its nearest palette color, so
// flat areas stay flat. Otherwise, as in photos, the image is dithered to
// avoid bands in smooth gradients.
func quantizeImage(img *image.RGBA) *image.Paletted {
	var buckets colorBuckets
	exact := make(map[[3]uint8]int)
	opaque := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i+3] < 128 {
			continue
		}
		opaque++
		// Premultiplied colors are the pixel drawn over black.
		c := [3]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2]}
		buckets.add(c, 1)
		if _, ok := exact[c]; ok || len(exact) < maxExactColorCount {
			exact[c]++
		}
	}

	out := image.NewPaletted(img.Bounds(), color.Palette{color.RGBA{}})
	if len(exact) <= 255 && len(exact) < maxExactColorCount {
		index := make(map[[3]uint8]uint8, len(exact))
		for c := range exact {
			index[c] = uint8(len(out.Palette))
			out.Palette = append(out.Palette, color.RGBA{c[0], c[1], c[2], 255})
		}
		for i := range out.Pix {
			p := img.Pix[i*4 : i*4+4]
			if p[3] >= 128 {
				out.Pix[i] = index[[3]uint8{p[0], p[1], p[2]}]
			}
		}
		return out
	}

	// Keep the most common colors exactly.
	common := make([][3]uint8, 0, len(exact))
	for c, n := range exact {
		if n*keptColorShare >= opaque {
			common = append(common, c)
		}
	}
	sort.Slice(common, func(i, j int) bool {
		if exact[common[i]] != exact[common[j]] {
			return exact[common[i]] > exact[common[j]]
		}
		return colorKey(common[i]) < colorKey(common[j])
	})
	common = common[:min(len(common), maxKeptColors)]
	keptIndex := make(map[[3]uint8]uint8, len(common))
	keptPixels := 0
	for _, c := range common {
		keptIndex[c] = uint8(len(out.Palette))
		out.Palette = append(out.Palette, color.RGBA{c[0], c[1], c[2], 255})
		keptPixels += exact[c]
		buckets.add(c, -exact[c])
	}
	for _, c := range buckets.medianCut(256 - len(out.Palette)) {
		out.Palette = append(out.Palette, color.RGBA{c[0], c[1], c[2], 255})
	}

	nearest := newNearestColors(out.Palette)
	if keptPixels*2 >= opaque {
		for i := range out.Pix {
			p := img.Pix[i*4 : i*4+4]
			if p[3] < 128 {
				continue
			}
			c := [3]uint8{p[0], p[1], p[2]}
			if index, ok := keptIndex[c]; ok {
				out.Pix[i] = index
			} else {
				out.Pix[i] = nearest.index(c)
			}
		}
		return out
	}
	ditherImage(img, out, nearest)
	return out
}

func colorKey(c [3]uint8) int {
	return int(c[0])<<16 | int(c[1])<<8 | int(c[2])
}

// colorBuckets counts colors in 32 x 32 x 32 buckets, with the sum of each
// bucket's colors so its average color is exact.
type colorBuckets struct {
	count [32768]int
	sum   [32768][3]int
}

func bucketOf(c [3]uint8) int {
	return int(c[0]>>3)<<10 | int(c[1]>>3)<<5 | int(c[2]>>3)
}

func (b *colorBuckets) add(c [3]uint8, n int) {
	i := bucketOf(c)
	b.count[i] += n
	for k := 0; k < 3; k++ {
		b.sum[i][k] += n * int(c[k])
	}
}

// medianCut chooses up to n colors for the counted colors: it splits them
// into groups, each time splitting the group with the most pixels times the
// widest spread in one of red, green, or blue at the middle pixel of that
// spread, and returns each group's average color.
func (b *colorBuckets) medianCut(n int) [][3]uint8 {
	type group struct {
		buckets []int
		pixels  int
	}
	var all []int
	pixels := 0
	for i, count := range b.count {
		if count > 0 {
			all = append(all, i)
			pixels += count
		}
	}
	if len(all) == 0 || n <= 0 {
		return nil
	}

	channel := func(bucket int, k int) int { return (bucket >> (10 - 5*k)) & 31 }
	spread := func(g group) (int, int) {
		widest, widestChannel := -1, 0
		for k := 0; k < 3; k++ {
			lo, hi := 31, 0
			for _, bucket := range g.buckets {
				v := channel(bucket, k)
				lo, hi = min(lo, v), max(hi, v)
			}
			if hi-lo > widest {
				widest, widestChannel = hi-lo, k
			}
		}
		return widest, widestChannel
	}

	groups := []group{{all, pixels}}
	for len(groups) < n {
		best, bestScore := -1, 0
		for i, g := range groups {
			if len(g.buckets) < 2 {
				continue
			}
			width, _ := spread(g)
			if score := g.pixels * width; score > bestScore {
				best, bestScore = i, score
			}
		}
		if best < 0 {
			break
		}
		g := groups[best]
		_, k := spread(g)
		sort.Slice(g.buckets, func(i, j int) bool { return channel(g.buckets[i], k) < channel(g.buckets[j], k) })
		half, seen, split := g.pixels/2, 0, 1
		for i, bucket := range g.buckets[:len(g.buckets)-1] {
			seen += b.count[bucket]
			split = i + 1
			if seen >= half {
				break
			}
		}
		first := group{buckets: g.buckets[:split]}
		second := group{buckets: g.buckets[split:]}
		for _, bucket := range first.buckets {
			first.pixels += b.count[bucket]
		}
		second.pixels = g.pixels - first.pixels
		groups[best] = first
		groups = append(groups, second)
	}

	colors := make([][3]uint8, 0, len(groups))
	for _, g := range groups {
		var sum [3]int
		for _, bucket := range g.buckets {
			for k := 0; k < 3; k++ {
				sum[k] += b.sum[bucket][k]
			}
		}
		if g.pixels > 0 {
			colors = append(colors, [3]uint8{uint8(sum[0] / g.pixels), uint8(sum[1] / g.pixels), uint8(sum[2] / g.pixels)})
		}
	}
	return colors
}

// nearestColors finds the nearest palette color, skipping index 0, which is
// transparent. Answers are remembered per 32 x 32 x 32 bucket, so colors in
// the same bucket share one answer: the nearest color to the bucket's center.
type nearestColors struct {
	palette color.Palette
	known   [32768]bool
	answer  [32768]uint8
}

func newNearestColors(palette color.Palette) *nearestColors {
	return &nearestColors{palette: palette}
}

func (n *nearestColors) index(c [3]uint8) uint8 {
	bucket := bucketOf(c)
	if n.known[bucket] {
		return n.answer[bucket]
	}
	center := [3]int{int(c[0]&^7) + 4, int(c[1]&^7) + 4, int(c[2]&^7) + 4}
	best, bestDistance := 1, math.MaxInt
	for i := 1; i < len(n.palette); i++ {
		p := n.palette[i].(color.RGBA)
		dr, dg, db := int(p.R)-center[0], int(p.G)-center[1], int(p.B)-center[2]
		if d := dr*dr + dg*dg + db*db; d < bestDistance {
			best, bestDistance = i, d
		}
	}
	n.known[bucket] = true
	n.answer[bucket] = uint8(best)
	return uint8(best)
}

// ditherImage maps img onto out's palette with Floyd-Steinberg dithering:
// each pixel's difference from its palette color is passed on to the pixels
// to its right and below. Transparent pixels are left at index 0 and pass
// nothing on.
func ditherImage(img *image.RGBA, out *image.Paletted, nearest *nearestColors) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	// Differences carried to this row and the next, per channel, with one
	// extra pixel on each side so the edges need no checks.
	this := make([]int32, (w+2)*3)
	next := make([]int32, (w+2)*3)
	for y := 0; y < h; y++ {
		clear(next)
		for x := 0; x < w; x++ {
			p := img.Pix[y*img.Stride+x*4 : y*img.Stride+x*4+4]
			if p[3] < 128 {
				continue
			}
			var c [3]uint8
			var want [3]int32
			for k := 0; k < 3; k++ {
				want[k] = int32(p[k]) + this[(x+1)*3+k]/16
				c[k] = uint8(min(max(want[k], 0), 255))
			}
			index := nearest.index(c)
			out.Pix[y*out.Stride+x] = index
			chosen := out.Palette[index].(color.RGBA)
			got := [3]int32{int32(chosen.R), int32(chosen.G), int32(chosen.B)}
			for k := 0; k < 3; k++ {
				diff := int32(c[k]) - got[k]
				this[(x+2)*3+k] += diff * 7
				next[x*3+k] += diff * 3
				next[(x+1)*3+k] += diff * 5
				next[(x+2)*3+k] += diff
			}
		}
		this, next = next, this
	}
}

// encodeSixel writes img as sixel data. Pixels with palette index 0 are not
// drawn, so the terminal background shows through them. It returns nil if
// the data would be longer than maxBytes.
func encodeSixel(img *image.Paletted, maxBytes int) []byte {
	var buf bytes.Buffer
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	// P2 = 1: pixels that are not drawn keep their current color.
	// The raster attributes give a 1:1 pixel aspect ratio and the image size.
	fmt.Fprintf(&buf, "\033P0;1;0q\"1;1;%d;%d", w, h)
	for i := 1; i < len(img.Palette); i++ {
		r, g, b, _ := img.Palette[i].RGBA()
		fmt.Fprintf(&buf, "#%d;2;%d;%d;%d", i, (r*100+0x7fff)/0xffff, (g*100+0x7fff)/0xffff, (b*100+0x7fff)/0xffff)
	}

	// Each sixel character draws a column of 6 pixels, so the image is
	// written in bands 6 pixels tall, one pass per color in the band.
	sixels := make([]byte, w)
	used := make([]bool, len(img.Palette))
	for top := 0; top < h; top += 6 {
		bandH := min(6, h-top)
		clear(used)
		for y := top; y < top+bandH; y++ {
			for _, p := range img.Pix[y*img.Stride : y*img.Stride+w] {
				used[p] = true
			}
		}
		first := true
		for ci := 1; ci < len(used); ci++ {
			if !used[ci] {
				continue
			}
			for x := 0; x < w; x++ {
				var bits byte
				for dy := 0; dy < bandH; dy++ {
					if int(img.Pix[(top+dy)*img.Stride+x]) == ci {
						bits |= 1 << dy
					}
				}
				sixels[x] = bits
			}
			if !first {
				buf.WriteByte('$') // back to the start of the band
			}
			first = false
			fmt.Fprintf(&buf, "#%d", ci)
			writeSixelRuns(&buf, sixels)
		}
		buf.WriteByte('-') // next band
		if buf.Len() > maxBytes {
			return nil
		}
	}
	buf.WriteString("\033\\")
	if buf.Len() > maxBytes {
		return nil
	}
	return buf.Bytes()
}

// writeSixelRuns writes one color's pass over a band. Repeated sixels are
// written as a count, and empty sixels at the end are left off.
func writeSixelRuns(buf *bytes.Buffer, sixels []byte) {
	end := len(sixels)
	for end > 0 && sixels[end-1] == 0 {
		end--
	}
	for i := 0; i < end; {
		j := i + 1
		for j < end && sixels[j] == sixels[i] {
			j++
		}
		ch := sixels[i] + '?'
		if n := j - i; n > 3 {
			fmt.Fprintf(buf, "!%d%c", n, ch)
		} else {
			for k := 0; k < n; k++ {
				buf.WriteByte(ch)
			}
		}
		i = j
	}
}
