package main

// Minimal PDF metadata reader for the file manager preview.
//
// It never reads a whole PDF. It reads the first few KB (header and version),
// the last few KB (startxref), the cross-reference data, and only the handful
// of objects it needs: the trailer, the Catalog, the page tree down to the
// first page, and the Info dictionary. Classic xref tables are not even read
// in full: entries are fixed width, so an object's entry is found by
// computing its offset. Cross-reference streams and object streams are
// compressed, so those are decompressed, with caps on how much.

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	pdfHeaderWindow     = 4096
	pdfTailWindow       = 4096
	pdfReadChunk        = 4096
	pdfMaxObjectBytes   = 1 << 20  // largest single object we will tokenize
	pdfMaxStreamBytes   = 16 << 20 // largest compressed stream we will read
	pdfMaxDecodedBytes  = 64 << 20 // largest decompressed stream we will keep
	pdfMaxXrefSections  = 256
	pdfMaxDepth         = 64
	pdfMaxResolveHops   = 32
	pdfMaxCachedObjStms = 8
)

var (
	errPdfSyntax      = errors.New("pdf syntax error")
	errPdfNotPdf      = errors.New("not a PDF file")
	errPdfTooLarge    = errors.New("pdf object too large")
	errPdfTooDeep     = errors.New("pdf nesting too deep")
	errPdfUnsupported = errors.New("unsupported pdf feature")
)

type pdfName string
type pdfString []byte
type pdfKeyword string
type pdfRef struct{ num, gen int }
type pdfDict map[pdfName]any
type pdfArray []any

// pdfInfo is what the preview shows. pages is -1 when unknown; width and
// height are in points (after applying /Rotate) and 0 when unknown.
type pdfInfo struct {
	version   string
	pages     int
	width     float64
	height    float64
	encrypted bool
	title     string
	author    string
	subject   string
	creator   string
	producer  string
	created   string
	modified  string
}

// ---------------------------------------------------------------------------
// Lexer / parser

type pdfLexer struct {
	buf    []byte
	pos    int
	origin int64       // file offset of buf[0]
	fill   func() bool // appends more bytes to buf; nil for a fixed buffer
	peeked []any       // pushed-back tokens, last is next
}

func isPdfWhite(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func isPdfDelim(c byte) bool {
	switch c {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	}
	return false
}

// byteAt returns the byte i positions past the cursor, reading more if needed.
func (l *pdfLexer) byteAt(i int) (byte, bool) {
	for l.pos+i >= len(l.buf) {
		if l.fill == nil || !l.fill() {
			return 0, false
		}
	}
	return l.buf[l.pos+i], true
}

func (l *pdfLexer) peekByte() (byte, bool) { return l.byteAt(0) }

func (l *pdfLexer) offset() int64 { return l.origin + int64(l.pos) }

func (l *pdfLexer) skipSpace() {
	for {
		c, ok := l.peekByte()
		if !ok {
			return
		}
		if isPdfWhite(c) {
			l.pos++
			continue
		}
		if c == '%' {
			for {
				c, ok = l.peekByte()
				if !ok || c == '\n' || c == '\r' {
					break
				}
				l.pos++
			}
			continue
		}
		return
	}
}

func (l *pdfLexer) unread(tok any) { l.peeked = append(l.peeked, tok) }

// next returns the next token: int64, float64, pdfName, pdfString, or
// pdfKeyword (which also covers "<<", ">>", "[", "]", "{", "}").
func (l *pdfLexer) next() (any, error) {
	if n := len(l.peeked); n > 0 {
		tok := l.peeked[n-1]
		l.peeked = l.peeked[:n-1]
		return tok, nil
	}
	l.skipSpace()
	c, ok := l.peekByte()
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	switch c {
	case '/':
		l.pos++
		return l.readName()
	case '(':
		l.pos++
		return l.readLiteralString()
	case '<':
		l.pos++
		if c2, ok := l.peekByte(); ok && c2 == '<' {
			l.pos++
			return pdfKeyword("<<"), nil
		}
		return l.readHexString()
	case '>':
		l.pos++
		if c2, ok := l.peekByte(); ok && c2 == '>' {
			l.pos++
			return pdfKeyword(">>"), nil
		}
		return nil, errPdfSyntax
	case '[', ']', '{', '}':
		l.pos++
		return pdfKeyword(string(c)), nil
	case ')':
		return nil, errPdfSyntax
	}
	return l.readRegular()
}

func (l *pdfLexer) readRegular() (any, error) {
	start := l.pos
	for {
		c, ok := l.peekByte()
		if !ok || isPdfWhite(c) || isPdfDelim(c) {
			break
		}
		l.pos++
	}
	word := string(l.buf[start:l.pos])
	if word == "" {
		return nil, errPdfSyntax
	}
	if isPdfNumberWord(word) {
		if i, err := strconv.ParseInt(word, 10, 64); err == nil {
			return i, nil
		}
		if f, err := strconv.ParseFloat(word, 64); err == nil {
			return f, nil
		}
	}
	return pdfKeyword(word), nil
}

func isPdfNumberWord(word string) bool {
	for i := 0; i < len(word); i++ {
		c := word[i]
		if (c < '0' || c > '9') && c != '.' && c != '-' && c != '+' {
			return false
		}
	}
	return true
}

func pdfHexValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func (l *pdfLexer) readName() (any, error) {
	var out []byte
	for {
		c, ok := l.peekByte()
		if !ok || isPdfWhite(c) || isPdfDelim(c) {
			break
		}
		l.pos++
		if c == '#' {
			h1, ok1 := l.byteAt(0)
			h2, ok2 := l.byteAt(1)
			v1, hex1 := pdfHexValue(h1)
			v2, hex2 := pdfHexValue(h2)
			if ok1 && ok2 && hex1 && hex2 {
				l.pos += 2
				out = append(out, v1<<4|v2)
				continue
			}
		}
		out = append(out, c)
	}
	return pdfName(out), nil
}

func (l *pdfLexer) readLiteralString() (any, error) {
	var out []byte
	depth := 1
	for {
		c, ok := l.peekByte()
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		l.pos++
		switch c {
		case '(':
			depth++
			out = append(out, c)
		case ')':
			depth--
			if depth == 0 {
				return pdfString(out), nil
			}
			out = append(out, c)
		case '\r':
			if c2, ok := l.peekByte(); ok && c2 == '\n' {
				l.pos++
			}
			out = append(out, '\n')
		case '\\':
			c2, ok := l.peekByte()
			if !ok {
				return nil, io.ErrUnexpectedEOF
			}
			l.pos++
			switch c2 {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				// Line continuation.
				if c3, ok := l.peekByte(); ok && c3 == '\n' {
					l.pos++
				}
			case '\n':
				// Line continuation.
			case '0', '1', '2', '3', '4', '5', '6', '7':
				v := int(c2 - '0')
				for range 2 {
					d, ok := l.peekByte()
					if !ok || d < '0' || d > '7' {
						break
					}
					l.pos++
					v = v*8 + int(d-'0')
				}
				out = append(out, byte(v))
			default:
				out = append(out, c2)
			}
		default:
			out = append(out, c)
		}
	}
}

func (l *pdfLexer) readHexString() (any, error) {
	var out []byte
	var hi byte
	half := false
	for {
		c, ok := l.peekByte()
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		l.pos++
		if c == '>' {
			if half {
				out = append(out, hi<<4)
			}
			return pdfString(out), nil
		}
		if isPdfWhite(c) {
			continue
		}
		v, ok := pdfHexValue(c)
		if !ok {
			return nil, errPdfSyntax
		}
		if half {
			out = append(out, hi<<4|v)
		} else {
			hi = v
		}
		half = !half
	}
}

// parseValue parses one PDF object, turning "n g R" into a pdfRef.
func (l *pdfLexer) parseValue(depth int) (any, error) {
	if depth > pdfMaxDepth {
		return nil, errPdfTooDeep
	}
	tok, err := l.next()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case int64:
		tok2, err := l.next()
		if err != nil {
			return t, nil
		}
		if gen, ok := tok2.(int64); ok {
			tok3, err := l.next()
			if err == nil {
				if kw, ok := tok3.(pdfKeyword); ok && kw == "R" {
					return pdfRef{num: int(t), gen: int(gen)}, nil
				}
				l.unread(tok3)
			}
		}
		l.unread(tok2)
		return t, nil
	case pdfKeyword:
		switch t {
		case "<<":
			d := pdfDict{}
			for {
				tok, err := l.next()
				if err != nil {
					return nil, err
				}
				if kw, ok := tok.(pdfKeyword); ok && kw == ">>" {
					return d, nil
				}
				key, ok := tok.(pdfName)
				if !ok {
					return nil, errPdfSyntax
				}
				val, err := l.parseValue(depth + 1)
				if err != nil {
					return nil, err
				}
				d[key] = val
			}
		case "[":
			var arr pdfArray
			for {
				tok, err := l.next()
				if err != nil {
					return nil, err
				}
				if kw, ok := tok.(pdfKeyword); ok && kw == "]" {
					return arr, nil
				}
				l.unread(tok)
				val, err := l.parseValue(depth + 1)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		return nil, errPdfSyntax
	}
	return tok, nil
}

// ---------------------------------------------------------------------------
// Value helpers

func pdfInt(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1<<53 {
			return int64(t), true
		}
	}
	return 0, false
}

func pdfNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case int64:
		return float64(t), true
	case float64:
		return t, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// File / cross-reference handling

type pdfXrefEntry struct {
	kind   int // 0 free, 1 at a file offset, 2 inside an object stream
	offset int64
	stmNum int
	stmIdx int
}

type pdfXrefSubsection struct {
	start  int
	count  int
	offset int64 // file offset of the first entry
	stride int64 // bytes per entry, normally 20
}

type pdfXrefStream struct {
	dict       pdfDict
	dataOffset int64
	w          [3]int
	index      []int // start/count pairs
	loaded     bool
	data       []byte
}

type pdfXrefSection struct {
	trailer     pdfDict // nil for the stream half of a hybrid file
	subsections []pdfXrefSubsection
	stream      *pdfXrefStream
}

type pdfObjStm struct {
	nums    []int
	offsets []int
	data    []byte // decoded bytes, starting at /First
}

type pdfFile struct {
	r         io.ReaderAt
	size      int64
	base      int64 // offset of "%PDF-"; xref offsets are relative to it
	sections  []*pdfXrefSection
	objects   map[int]any
	resolving map[int]bool
	objStms   map[int]*pdfObjStm
}

func newPdfFile(r io.ReaderAt, size int64, base int64) *pdfFile {
	return &pdfFile{
		r:         r,
		size:      size,
		base:      base,
		objects:   map[int]any{},
		resolving: map[int]bool{},
		objStms:   map[int]*pdfObjStm{},
	}
}

func (p *pdfFile) readAt(offset int64, n int) []byte {
	if offset < 0 || offset >= p.size || n <= 0 {
		return nil
	}
	if remaining := p.size - offset; int64(n) > remaining {
		n = int(remaining)
	}
	buf := make([]byte, n)
	got, _ := p.r.ReadAt(buf, offset)
	return buf[:got]
}

// lexerAt returns a lexer that reads the file from offset in growing chunks,
// up to pdfMaxObjectBytes.
func (p *pdfFile) lexerAt(offset int64) *pdfLexer {
	l := &pdfLexer{origin: offset}
	l.fill = func() bool {
		if len(l.buf) >= pdfMaxObjectBytes {
			return false
		}
		chunk := max(pdfReadChunk, len(l.buf))
		data := p.readAt(offset+int64(len(l.buf)), chunk)
		if len(data) == 0 {
			return false
		}
		l.buf = append(l.buf, data...)
		return true
	}
	return l
}

// readObjectAt parses "num gen obj <value>" at an absolute file offset. If
// the value is a stream, the absolute offset of its data is returned, else -1.
// wantNum < 0 skips the object number check.
func (p *pdfFile) readObjectAt(offset int64, wantNum int) (any, int64, error) {
	l := p.lexerAt(offset)
	numTok, err := l.next()
	if err != nil {
		return nil, -1, err
	}
	genTok, err := l.next()
	if err != nil {
		return nil, -1, err
	}
	objTok, err := l.next()
	if err != nil {
		return nil, -1, err
	}
	num, ok1 := numTok.(int64)
	_, ok2 := genTok.(int64)
	kw, ok3 := objTok.(pdfKeyword)
	if !ok1 || !ok2 || !ok3 || kw != "obj" {
		return nil, -1, errPdfSyntax
	}
	if wantNum >= 0 && int(num) != wantNum {
		return nil, -1, fmt.Errorf("pdf xref points at object %d, wanted %d", num, wantNum)
	}
	val, err := l.parseValue(0)
	if err != nil {
		return nil, -1, err
	}
	if _, isDict := val.(pdfDict); !isDict {
		return val, -1, nil
	}
	tok, err := l.next()
	if err != nil {
		return val, -1, nil
	}
	if kw, ok := tok.(pdfKeyword); !ok || kw != "stream" {
		return val, -1, nil
	}
	// Stream data starts after the EOL following "stream".
	if c, ok := l.peekByte(); ok && c == '\r' {
		l.pos++
	}
	if c, ok := l.peekByte(); ok && c == '\n' {
		l.pos++
	}
	return val, l.offset(), nil
}

// streamData reads and decodes a stream whose data begins at dataOffset.
func (p *pdfFile) streamData(dict pdfDict, dataOffset int64, lengthMustBeDirect bool) ([]byte, error) {
	lengthVal := dict["Length"]
	if !lengthMustBeDirect {
		lengthVal = p.resolve(lengthVal)
	}
	length, ok := pdfInt(lengthVal)
	if !ok || length < 0 {
		return nil, errPdfSyntax
	}
	if length > pdfMaxStreamBytes {
		return nil, errPdfTooLarge
	}
	if dataOffset+length > p.size {
		return nil, io.ErrUnexpectedEOF
	}
	raw := p.readAt(dataOffset, int(length))
	if int64(len(raw)) != length {
		return nil, io.ErrUnexpectedEOF
	}
	return p.decodeStream(dict, raw)
}

func (p *pdfFile) decodeStream(dict pdfDict, raw []byte) ([]byte, error) {
	filter := p.resolve(dict["Filter"])
	params := p.resolve(dict["DecodeParms"])
	if arr, ok := filter.(pdfArray); ok {
		if len(arr) == 0 {
			filter = nil
		} else if len(arr) == 1 {
			filter = p.resolve(arr[0])
		} else {
			return nil, errPdfUnsupported
		}
		if parr, ok := params.(pdfArray); ok && len(parr) > 0 {
			params = p.resolve(parr[0])
		}
	}
	if filter == nil {
		return raw, nil
	}
	name, ok := filter.(pdfName)
	if !ok || (name != "FlateDecode" && name != "Fl") {
		return nil, errPdfUnsupported
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	data, err := io.ReadAll(io.LimitReader(zr, pdfMaxDecodedBytes+1))
	if len(data) > pdfMaxDecodedBytes {
		return nil, errPdfTooLarge
	}
	// Tolerate a truncated or checksum-less zlib stream if it produced data.
	if err != nil && len(data) == 0 {
		return nil, err
	}
	if pd, ok := params.(pdfDict); ok {
		return p.applyPredictor(pd, data)
	}
	return data, nil
}

// applyPredictor undoes the PNG predictors (10-15) commonly used on
// cross-reference streams.
func (p *pdfFile) applyPredictor(params pdfDict, data []byte) ([]byte, error) {
	predictor, _ := pdfInt(p.resolve(params["Predictor"]))
	if predictor <= 1 {
		return data, nil
	}
	if predictor < 10 {
		return nil, errPdfUnsupported
	}
	colors, bpc, columns := int64(1), int64(8), int64(1)
	if v, ok := pdfInt(p.resolve(params["Colors"])); ok {
		colors = v
	}
	if v, ok := pdfInt(p.resolve(params["BitsPerComponent"])); ok {
		bpc = v
	}
	if v, ok := pdfInt(p.resolve(params["Columns"])); ok {
		columns = v
	}
	if colors < 1 || colors > 64 || bpc < 1 || bpc > 16 || columns < 1 || columns > 1<<20 {
		return nil, errPdfSyntax
	}
	bpp := int((colors*bpc + 7) / 8)
	rowLen := int((colors*bpc*columns + 7) / 8)
	out := make([]byte, 0, len(data)/(rowLen+1)*rowLen)
	prev := make([]byte, rowLen)
	for i := 0; i+1+rowLen <= len(data); i += 1 + rowLen {
		filterType := data[i]
		row := append([]byte(nil), data[i+1:i+1+rowLen]...)
		for j := range row {
			var left, up, upLeft byte
			if j >= bpp {
				left = row[j-bpp]
				upLeft = prev[j-bpp]
			}
			up = prev[j]
			switch filterType {
			case 0:
			case 1:
				row[j] += left
			case 2:
				row[j] += up
			case 3:
				row[j] += byte((int(left) + int(up)) / 2)
			case 4:
				row[j] += pdfPaeth(left, up, upLeft)
			default:
				return nil, errPdfSyntax
			}
		}
		out = append(out, row...)
		prev = row
	}
	return out, nil
}

func pdfPaeth(a, b, c byte) byte {
	pa := int(b) - int(c)
	pb := int(a) - int(c)
	pc := pa + pb
	if pa < 0 {
		pa = -pa
	}
	if pb < 0 {
		pb = -pb
	}
	if pc < 0 {
		pc = -pc
	}
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

// loadXref follows the chain of cross-reference sections starting at the
// file offset from startxref, newest first.
func (p *pdfFile) loadXref(startOffset int64) error {
	seen := map[int64]bool{}
	offset := startOffset
	for len(p.sections) < pdfMaxXrefSections {
		if seen[offset] {
			break
		}
		seen[offset] = true
		sec, err := p.readXrefSection(offset)
		if err != nil {
			if len(p.sections) == 0 {
				return err
			}
			// Keep the newer sections we could read.
			break
		}
		p.sections = append(p.sections, sec)
		// Hybrid files: a classic table whose trailer points at an xref
		// stream holding the compressed objects.
		if sec.stream == nil {
			if xs, ok := pdfInt(sec.trailer["XRefStm"]); ok {
				if stmSec, err := p.readXrefSection(p.base + xs); err == nil && stmSec.stream != nil {
					stmSec.trailer = nil
					p.sections = append(p.sections, stmSec)
				}
			}
		}
		prev, ok := pdfInt(sec.trailer["Prev"])
		if !ok {
			break
		}
		offset = p.base + prev
	}
	return nil
}

func (p *pdfFile) readXrefSection(offset int64) (*pdfXrefSection, error) {
	l := p.lexerAt(offset)
	l.skipSpace()
	if b0, _ := l.byteAt(0); b0 == 'x' {
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		if kw, ok := tok.(pdfKeyword); ok && kw == "xref" {
			return p.readClassicXref(l)
		}
		return nil, errPdfSyntax
	}
	val, dataOffset, err := p.readObjectAt(offset, -1)
	if err != nil {
		return nil, err
	}
	dict, ok := val.(pdfDict)
	if !ok || dataOffset < 0 {
		return nil, errPdfSyntax
	}
	if typ, _ := dict["Type"].(pdfName); typ != "XRef" {
		return nil, errPdfSyntax
	}
	stream := &pdfXrefStream{dict: dict, dataOffset: dataOffset}
	wArr, ok := dict["W"].(pdfArray)
	if !ok || len(wArr) != 3 {
		return nil, errPdfSyntax
	}
	for i := range 3 {
		w, ok := pdfInt(wArr[i])
		if !ok || w < 0 || w > 8 {
			return nil, errPdfSyntax
		}
		stream.w[i] = int(w)
	}
	if idxArr, ok := dict["Index"].(pdfArray); ok {
		for _, v := range idxArr {
			n, ok := pdfInt(v)
			if !ok || n < 0 {
				return nil, errPdfSyntax
			}
			stream.index = append(stream.index, int(n))
		}
		if len(stream.index)%2 != 0 {
			return nil, errPdfSyntax
		}
	} else {
		size, ok := pdfInt(dict["Size"])
		if !ok || size < 0 {
			return nil, errPdfSyntax
		}
		stream.index = []int{0, int(size)}
	}
	return &pdfXrefSection{trailer: dict, stream: stream}, nil
}

// readClassicXref reads subsection headers and the trailer, skipping over
// the entries themselves. l is positioned just after "xref".
func (p *pdfFile) readClassicXref(l *pdfLexer) (*pdfXrefSection, error) {
	sec := &pdfXrefSection{}
	for range 100000 {
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		if kw, ok := tok.(pdfKeyword); ok && kw == "trailer" {
			val, err := l.parseValue(0)
			if err != nil {
				return nil, err
			}
			dict, ok := val.(pdfDict)
			if !ok {
				return nil, errPdfSyntax
			}
			sec.trailer = dict
			return sec, nil
		}
		start, ok1 := tok.(int64)
		countTok, err := l.next()
		if err != nil {
			return nil, err
		}
		count, ok2 := countTok.(int64)
		if !ok1 || !ok2 || start < 0 || count < 0 || count > 1<<31 {
			return nil, errPdfSyntax
		}
		l.skipSpace()
		entriesOffset := l.offset()
		// Entries should be exactly 20 bytes, but some writers use a
		// one-byte EOL (19) or add an extra byte (21). Measure the first.
		stride := int64(18)
		for {
			c, ok := l.byteAt(int(stride))
			if !ok || !isPdfWhite(c) {
				break
			}
			stride++
		}
		if stride < 19 || stride > 21 {
			stride = 20
		}
		sec.subsections = append(sec.subsections, pdfXrefSubsection{
			start:  int(start),
			count:  int(count),
			offset: entriesOffset,
			stride: stride,
		})
		// Skip the entries. Reuse the bytes already read when the next
		// subsection header is inside them, so that many small subsections
		// do not each cost a new read.
		next := entriesOffset + count*stride
		if next-l.origin <= int64(len(l.buf)) {
			l.pos = int(next - l.origin)
		} else {
			l = p.lexerAt(next)
		}
	}
	return nil, errPdfSyntax
}

func (s *pdfXrefSection) lookup(p *pdfFile, num int) (pdfXrefEntry, bool) {
	if s.stream != nil {
		return s.stream.lookup(p, num)
	}
	for _, sub := range s.subsections {
		if num < sub.start || num >= sub.start+sub.count {
			continue
		}
		raw := p.readAt(sub.offset+int64(num-sub.start)*sub.stride, int(sub.stride))
		fields := bytes.Fields(raw)
		if len(fields) < 3 {
			return pdfXrefEntry{}, false
		}
		off, err := strconv.ParseInt(string(fields[0]), 10, 64)
		if err != nil {
			return pdfXrefEntry{}, false
		}
		if fields[2][0] == 'n' {
			return pdfXrefEntry{kind: 1, offset: off}, true
		}
		return pdfXrefEntry{kind: 0}, true
	}
	return pdfXrefEntry{}, false
}

func (s *pdfXrefStream) lookup(p *pdfFile, num int) (pdfXrefEntry, bool) {
	if !s.loaded {
		s.loaded = true
		data, err := p.streamData(s.dict, s.dataOffset, true)
		if err == nil {
			s.data = data
		}
	}
	rowLen := s.w[0] + s.w[1] + s.w[2]
	if rowLen == 0 {
		return pdfXrefEntry{}, false
	}
	row := 0
	for i := 0; i+1 < len(s.index); i += 2 {
		start, count := s.index[i], s.index[i+1]
		if num >= start && num < start+count {
			at := (row + num - start) * rowLen
			if at+rowLen > len(s.data) {
				return pdfXrefEntry{}, false
			}
			field := func(from, width int) int64 {
				var v int64
				for _, b := range s.data[from : from+width] {
					v = v<<8 | int64(b)
				}
				return v
			}
			kind := int64(1)
			if s.w[0] > 0 {
				kind = field(at, s.w[0])
			}
			f2 := field(at+s.w[0], s.w[1])
			f3 := field(at+s.w[0]+s.w[1], s.w[2])
			switch kind {
			case 1:
				return pdfXrefEntry{kind: 1, offset: f2}, true
			case 2:
				return pdfXrefEntry{kind: 2, stmNum: int(f2), stmIdx: int(f3)}, true
			}
			return pdfXrefEntry{kind: 0}, true
		}
		row += count
	}
	return pdfXrefEntry{}, false
}

// lookup finds the newest in-use entry for an object number.
func (p *pdfFile) lookup(num int) (pdfXrefEntry, bool) {
	for _, sec := range p.sections {
		if e, found := sec.lookup(p, num); found && e.kind != 0 {
			return e, true
		}
	}
	return pdfXrefEntry{}, false
}

// object loads an indirect object by number. Missing or unreadable objects
// are treated as null, as the spec says for missing objects.
func (p *pdfFile) object(num int) any {
	if v, ok := p.objects[num]; ok {
		return v
	}
	if p.resolving[num] {
		return nil
	}
	p.resolving[num] = true
	defer delete(p.resolving, num)

	var val any
	if e, found := p.lookup(num); found {
		switch e.kind {
		case 1:
			val, _, _ = p.readObjectAt(p.base+e.offset, num)
		case 2:
			val = p.objectFromStream(e.stmNum, e.stmIdx, num)
		}
	}
	p.objects[num] = val
	return val
}

func (p *pdfFile) objectFromStream(stmNum, idx, num int) any {
	stm, ok := p.objStms[stmNum]
	if !ok {
		stm = p.loadObjStm(stmNum)
		if len(p.objStms) >= pdfMaxCachedObjStms {
			clear(p.objStms)
		}
		p.objStms[stmNum] = stm
	}
	if stm == nil {
		return nil
	}
	i := idx
	if i < 0 || i >= len(stm.nums) || stm.nums[i] != num {
		i = -1
		for j, n := range stm.nums {
			if n == num {
				i = j
				break
			}
		}
		if i < 0 {
			return nil
		}
	}
	off := stm.offsets[i]
	if off < 0 || off >= len(stm.data) {
		return nil
	}
	l := &pdfLexer{buf: stm.data[off:]}
	val, err := l.parseValue(0)
	if err != nil {
		return nil
	}
	return val
}

func (p *pdfFile) loadObjStm(stmNum int) *pdfObjStm {
	e, found := p.lookup(stmNum)
	if !found || e.kind != 1 {
		return nil
	}
	val, dataOffset, err := p.readObjectAt(p.base+e.offset, stmNum)
	if err != nil || dataOffset < 0 {
		return nil
	}
	dict, ok := val.(pdfDict)
	if !ok {
		return nil
	}
	n, ok1 := pdfInt(p.resolve(dict["N"]))
	first, ok2 := pdfInt(p.resolve(dict["First"]))
	if !ok1 || !ok2 || n < 0 || first < 0 {
		return nil
	}
	data, err := p.streamData(dict, dataOffset, false)
	if err != nil || int64(len(data)) < first {
		return nil
	}
	stm := &pdfObjStm{data: data[first:]}
	l := &pdfLexer{buf: data[:first]}
	for range n {
		numTok, err1 := l.next()
		offTok, err2 := l.next()
		objNum, ok1 := numTok.(int64)
		off, ok2 := offTok.(int64)
		if err1 != nil || err2 != nil || !ok1 || !ok2 {
			break
		}
		stm.nums = append(stm.nums, int(objNum))
		stm.offsets = append(stm.offsets, int(off))
	}
	return stm
}

// resolve follows indirect references.
func (p *pdfFile) resolve(v any) any {
	for range pdfMaxResolveHops {
		ref, ok := v.(pdfRef)
		if !ok {
			return v
		}
		v = p.object(ref.num)
	}
	return nil
}

func (p *pdfFile) resolveDict(v any) pdfDict {
	d, _ := p.resolve(v).(pdfDict)
	return d
}

// trailerValue returns key from the newest trailer that has it.
func (p *pdfFile) trailerValue(key pdfName) any {
	for _, sec := range p.sections {
		if sec.trailer == nil {
			continue
		}
		if v, ok := sec.trailer[key]; ok {
			return v
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Top level

// readPdfInfo reads summary information from a PDF. It returns an error
// only when the data does not look like a PDF at all; otherwise fields that
// could not be read are left empty (pages -1).
func readPdfInfo(r io.ReaderAt, size int64) (*pdfInfo, error) {
	probe := newPdfFile(r, size, 0)
	head := probe.readAt(0, pdfHeaderWindow)
	headerAt := bytes.Index(head[:min(len(head), 1024)], []byte("%PDF-"))
	if headerAt < 0 {
		return nil, errPdfNotPdf
	}
	info := &pdfInfo{pages: -1}
	info.version = pdfHeaderVersion(head[headerAt+5:])

	tail := probe.readAt(max(0, size-pdfTailWindow), pdfTailWindow)
	startxref, haveStartxref := pdfFindStartxref(tail)

	var p *pdfFile
	if haveStartxref {
		// Offsets are normally relative to the header, which is usually at
		// byte 0. If junk precedes the header, try both interpretations.
		bases := []int64{int64(headerAt)}
		if headerAt != 0 {
			bases = append(bases, 0)
		}
		for _, base := range bases {
			candidate := newPdfFile(r, size, base)
			if err := candidate.loadXref(base + startxref); err == nil {
				p = candidate
				break
			}
		}
	}

	if p != nil {
		p.fillInfo(info)
	}
	if info.pages < 0 {
		if n, ok := pdfLinearizedPageCount(head[headerAt:], size); ok {
			info.pages = n
		}
	}
	return info, nil
}

func pdfHeaderVersion(afterHeader []byte) string {
	end := 0
	for end < len(afterHeader) && end < 8 {
		c := afterHeader[end]
		if (c < '0' || c > '9') && c != '.' {
			break
		}
		end++
	}
	return string(afterHeader[:end])
}

func pdfFindStartxref(tail []byte) (int64, bool) {
	at := bytes.LastIndex(tail, []byte("startxref"))
	if at < 0 {
		return 0, false
	}
	l := &pdfLexer{buf: tail[at+len("startxref"):]}
	tok, err := l.next()
	if err != nil {
		return 0, false
	}
	n, ok := tok.(int64)
	return n, ok && n >= 0
}

// pdfLinearizedPageCount reads /N from a linearization dictionary, which
// is the first object in a linearized file. It is only trusted when /L
// matches the file size, since an incremental update makes it stale.
func pdfLinearizedPageCount(head []byte, size int64) (int, bool) {
	l := &pdfLexer{buf: head}
	for _, want := range []string{"int", "int", "obj"} {
		tok, err := l.next()
		if err != nil {
			return 0, false
		}
		if want == "int" {
			if _, ok := tok.(int64); !ok {
				return 0, false
			}
		} else if kw, ok := tok.(pdfKeyword); !ok || kw != "obj" {
			return 0, false
		}
	}
	val, err := l.parseValue(0)
	if err != nil {
		return 0, false
	}
	dict, ok := val.(pdfDict)
	if !ok {
		return 0, false
	}
	if _, ok := dict["Linearized"]; !ok {
		return 0, false
	}
	length, ok1 := pdfInt(dict["L"])
	n, ok2 := pdfInt(dict["N"])
	if !ok1 || !ok2 || length != size || n < 0 {
		return 0, false
	}
	return int(n), true
}

func (p *pdfFile) fillInfo(info *pdfInfo) {
	info.encrypted = p.trailerValue("Encrypt") != nil

	catalog := p.resolveDict(p.trailerValue("Root"))
	if catalog != nil {
		if v, ok := p.resolve(catalog["Version"]).(pdfName); ok && pdfVersionGreater(string(v), info.version) {
			info.version = string(v)
		}
		pages := p.resolveDict(catalog["Pages"])
		if pages != nil {
			if n, ok := pdfInt(p.resolve(pages["Count"])); ok && n >= 0 {
				info.pages = int(n)
			}
			info.width, info.height = p.firstPageSize(pages)
		}
	}

	// With encryption, Info strings are encrypted too.
	if info.encrypted {
		return
	}
	infoDict := p.resolveDict(p.trailerValue("Info"))
	if infoDict == nil {
		return
	}
	text := func(key pdfName) string {
		if s, ok := p.resolve(infoDict[key]).(pdfString); ok {
			return decodePdfText(s)
		}
		return ""
	}
	info.title = text("Title")
	info.author = text("Author")
	info.subject = text("Subject")
	info.creator = text("Creator")
	info.producer = text("Producer")
	info.created = formatPdfDate(text("CreationDate"))
	info.modified = formatPdfDate(text("ModDate"))
}

func pdfVersionGreater(a, b string) bool {
	fa, errA := strconv.ParseFloat(a, 64)
	fb, errB := strconv.ParseFloat(b, 64)
	return errA == nil && (errB != nil || fa > fb)
}

// firstPageSize walks the page tree to the first page, carrying the
// inheritable CropBox/MediaBox/Rotate attributes along the way.
func (p *pdfFile) firstPageSize(root pdfDict) (float64, float64) {
	var mediaBox, cropBox any
	var rotate int64
	node := root
	for range pdfMaxDepth {
		if v, ok := node["MediaBox"]; ok {
			mediaBox = v
		}
		if v, ok := node["CropBox"]; ok {
			cropBox = v
		}
		if v, ok := pdfInt(p.resolve(node["Rotate"])); ok {
			rotate = v
		}
		if typ, _ := p.resolve(node["Type"]).(pdfName); typ == "Page" {
			break
		}
		kids, _ := p.resolve(node["Kids"]).(pdfArray)
		if len(kids) == 0 {
			break
		}
		next := p.resolveDict(kids[0])
		if next == nil {
			break
		}
		node = next
	}
	w, h, ok := p.boxSize(cropBox)
	if !ok {
		w, h, ok = p.boxSize(mediaBox)
	}
	if !ok {
		return 0, 0
	}
	if r := ((rotate % 360) + 360) % 360; r == 90 || r == 270 {
		w, h = h, w
	}
	return w, h
}

func (p *pdfFile) boxSize(v any) (float64, float64, bool) {
	arr, ok := p.resolve(v).(pdfArray)
	if !ok || len(arr) != 4 {
		return 0, 0, false
	}
	var n [4]float64
	for i := range 4 {
		f, ok := pdfNumber(p.resolve(arr[i]))
		if !ok {
			return 0, 0, false
		}
		n[i] = f
	}
	w, h := math.Abs(n[2]-n[0]), math.Abs(n[3]-n[1])
	if w == 0 || h == 0 {
		return 0, 0, false
	}
	return w, h, true
}

// ---------------------------------------------------------------------------
// Text

// pdfDocEncodingHigh maps PDFDocEncoding bytes that differ from Latin-1.
var pdfDocEncodingHigh = map[byte]rune{
	0x18: '˘', 0x19: 'ˇ', 0x1A: 'ˆ', 0x1B: '˙',
	0x1C: '˝', 0x1D: '˛', 0x1E: '˚', 0x1F: '˜',
	0x80: '•', 0x81: '†', 0x82: '‡', 0x83: '…',
	0x84: '—', 0x85: '–', 0x86: 'ƒ', 0x87: '⁄',
	0x88: '‹', 0x89: '›', 0x8A: '−', 0x8B: '‰',
	0x8C: '„', 0x8D: '“', 0x8E: '”', 0x8F: '‘',
	0x90: '’', 0x91: '‚', 0x92: '™', 0x93: 'ﬁ',
	0x94: 'ﬂ', 0x95: 'Ł', 0x96: 'Œ', 0x97: 'Š',
	0x98: 'Ÿ', 0x99: 'Ž', 0x9A: 'ı', 0x9B: 'ł',
	0x9C: 'œ', 0x9D: 'š', 0x9E: 'ž', 0xA0: '€',
}

// decodePdfText decodes a PDF text string (UTF-16BE with BOM, UTF-8 with
// BOM, or PDFDocEncoding) and removes control characters so the result is
// safe to print to a terminal.
func decodePdfText(b []byte) string {
	var s string
	switch {
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		units := make([]uint16, 0, (len(b)-2)/2)
		for i := 2; i+1 < len(b); i += 2 {
			units = append(units, uint16(b[i])<<8|uint16(b[i+1]))
		}
		s = string(utf16.Decode(units))
		// Language tags are wrapped in ESC ... ESC.
		for {
			startTag := strings.IndexRune(s, 0x1B)
			if startTag < 0 {
				break
			}
			endTag := strings.IndexRune(s[startTag+1:], 0x1B)
			if endTag < 0 {
				s = s[:startTag]
				break
			}
			s = s[:startTag] + s[startTag+1+endTag+1:]
		}
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		s = strings.ToValidUTF8(string(b[3:]), "�")
	default:
		var sb strings.Builder
		for _, c := range b {
			if r, ok := pdfDocEncodingHigh[c]; ok {
				sb.WriteRune(r)
			} else {
				sb.WriteRune(rune(c))
			}
		}
		s = sb.String()
	}

	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			sb.WriteByte(' ')
		case unicode.IsControl(r), r == utf8.RuneError:
		case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
			// Bidi overrides can reorder the rest of the terminal line.
		default:
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}

// formatPdfDate turns "D:YYYYMMDDHHmmSSOHH'mm'" into "YYYY-MM-DD HH:MM",
// adding the UTC offset when present. Unparseable input is returned as is.
func formatPdfDate(s string) string {
	if s == "" {
		return ""
	}
	raw := strings.TrimPrefix(s, "D:")
	digits := 0
	for digits < len(raw) && digits < 14 && raw[digits] >= '0' && raw[digits] <= '9' {
		digits++
	}
	if digits < 4 {
		return s
	}
	part := func(from, width, def int) int {
		if from+width > digits {
			return def
		}
		v, _ := strconv.Atoi(raw[from : from+width])
		return v
	}
	year, month, day := part(0, 4, 0), part(4, 2, 1), part(6, 2, 1)
	hour, minute := part(8, 2, 0), part(10, 2, 0)
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 {
		return s
	}
	out := fmt.Sprintf("%04d-%02d-%02d", year, month, day)
	if digits >= 12 {
		out += fmt.Sprintf(" %02d:%02d", hour, minute)
	}
	zone := strings.TrimSpace(raw[digits:])
	switch {
	case zone == "":
	case zone[0] == 'Z':
		out += " UTC"
	case zone[0] == '+' || zone[0] == '-':
		zdigits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, zone[1:])
		if len(zdigits) >= 2 {
			zh := zdigits[:2]
			zm := "00"
			if len(zdigits) >= 4 {
				zm = zdigits[2:4]
			}
			out += fmt.Sprintf(" %c%s:%s", zone[0], zh, zm)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Preview

func isPdfPreviewPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

func previewPdf(path string, maxLines int) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{" (cannot open for reading)"}
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return []string{" (cannot read)"}
	}
	info, err := readPdfInfo(f, stat.Size())
	if err != nil {
		return []string{" (not a valid PDF)"}
	}
	return truncatePreviewLines(formatPdfInfoLines(info), maxLines)
}

func formatPdfInfoLines(info *pdfInfo) []string {
	lines := []string{" PDF " + info.version}
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, fmt.Sprintf(" %-11s%s", label+":", value))
		}
	}
	if info.pages >= 0 {
		add("Pages", strconv.Itoa(info.pages))
	} else {
		add("Pages", "unknown")
	}
	if info.width > 0 && info.height > 0 {
		add("Page size", formatPdfPageSize(info.width, info.height))
	}
	if info.encrypted {
		add("Encrypted", "yes")
	}
	add("Title", info.title)
	add("Author", info.author)
	add("Subject", info.subject)
	add("Creator", info.creator)
	add("Producer", info.producer)
	add("Created", info.created)
	add("Modified", info.modified)
	return lines
}

var pdfPaperSizes = []struct {
	name string
	w, h float64 // portrait, points
}{
	{"Letter", 612, 792},
	{"Legal", 612, 1008},
	{"Tabloid", 792, 1224},
	{"Executive", 522, 756},
	{"A3", 842, 1191},
	{"A4", 595, 842},
	{"A5", 420, 595},
}

func formatPdfPageSize(w, h float64) string {
	trim := func(v float64) string {
		return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', 2, 64), "0"), ".")
	}
	dims := fmt.Sprintf("%s × %s in, %s × %s mm",
		trim(w/72), trim(h/72),
		strconv.Itoa(int(math.Round(w/72*25.4))), strconv.Itoa(int(math.Round(h/72*25.4))))
	shortSide, longSide := math.Min(w, h), math.Max(w, h)
	for _, paper := range pdfPaperSizes {
		if math.Abs(shortSide-paper.w) <= 2 && math.Abs(longSide-paper.h) <= 2 {
			name := paper.name
			if w > h {
				name += " landscape"
			}
			return name + " (" + dims + ")"
		}
	}
	return dims
}
