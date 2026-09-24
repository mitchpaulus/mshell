package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type countingReaderAt struct {
	r     *bytes.Reader
	bytes atomic.Int64
}

func (c *countingReaderAt) ReadAt(b []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(b, off)
	c.bytes.Add(int64(n))
	return n, err
}

func readPdfInfoBytes(t *testing.T, data []byte) (*pdfInfo, int64) {
	t.Helper()
	c := &countingReaderAt{r: bytes.NewReader(data)}
	info, err := readPdfInfo(c, int64(len(data)))
	if err != nil {
		t.Fatalf("readPdfInfo: %v", err)
	}
	return info, c.bytes.Load()
}

// buildTestPdf writes a PDF with a classic xref table. objs[i] is the body of
// object i+1. padding bytes of comment are placed before the objects, and eol
// ends each xref entry ("\r\n" gives 20-byte entries, "\n" gives 19).
func buildTestPdf(objs []string, trailer string, padding int, eol string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.6\n")
	if padding > 0 {
		b.WriteString("%" + strings.Repeat("x", padding) + "\n")
	}
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objs)+1)
	fmt.Fprintf(&b, "%010d %05d f%s", 0, 65535, eol)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d %05d n%s", off, 0, eol)
	}
	fmt.Fprintf(&b, "trailer\n%s\nstartxref\n%d\n%%%%EOF\n", trailer, xref)
	return b.Bytes()
}

func TestPdfInfoFixtures(t *testing.T) {
	for _, name := range []string{"classic.pdf", "objstm.pdf", "linearized.pdf"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "pdf", name))
			if err != nil {
				t.Fatal(err)
			}
			info, _ := readPdfInfoBytes(t, data)
			if info.version != "1.4" && info.version != "1.5" {
				t.Errorf("version = %q", info.version)
			}
			if info.pages != 3 {
				t.Errorf("pages = %d, want 3", info.pages)
			}
			// A4 inherited from the Pages node, first page rotated 90.
			if info.width != 842 || info.height != 595 {
				t.Errorf("size = %vx%v, want 842x595", info.width, info.height)
			}
			if info.encrypted {
				t.Errorf("encrypted = true")
			}
			if info.title != "Hi ɣ" {
				t.Errorf("title = %q", info.title)
			}
			if info.author != "Café (R)™" {
				t.Errorf("author = %q", info.author)
			}
			if info.producer != "hand written" {
				t.Errorf("producer = %q", info.producer)
			}
			if info.created != "2024-01-02 03:04 -05:00" {
				t.Errorf("created = %q", info.created)
			}
			if info.modified != "2024-02-03 UTC" {
				t.Errorf("modified = %q", info.modified)
			}
		})
	}
}

func TestPdfInfoEncrypted(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "pdf", "encrypted.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := readPdfInfoBytes(t, data)
	if !info.encrypted {
		t.Errorf("encrypted = false")
	}
	if info.pages != 3 {
		t.Errorf("pages = %d, want 3", info.pages)
	}
	if info.title != "" || info.author != "" {
		t.Errorf("encrypted strings should be skipped, got title %q author %q", info.title, info.author)
	}
}

func TestPdfLinearizedPageCount(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "pdf", "linearized.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	n, ok := pdfLinearizedPageCount(data, int64(len(data)))
	if !ok || n != 3 {
		t.Errorf("linearized count = %d, %v", n, ok)
	}
	// A size mismatch means the file was updated after linearization.
	if _, ok := pdfLinearizedPageCount(data, int64(len(data))+10); ok {
		t.Errorf("stale linearization dictionary was trusted")
	}
}

func TestPdfInfoReadsLittleOfLargeFile(t *testing.T) {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Title (Big) >>",
	}
	data := buildTestPdf(objs, "<< /Root 1 0 R /Info 4 0 R /Size 5 >>", 8<<20, "\r\n")
	info, read := readPdfInfoBytes(t, data)
	if info.pages != 1 || info.title != "Big" || info.width != 612 || info.height != 792 {
		t.Errorf("unexpected info %+v", info)
	}
	if read > 64<<10 {
		t.Errorf("read %d bytes of a %d byte file", read, len(data))
	}
}

func TestPdfInfoNineteenByteXrefEntries(t *testing.T) {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Author (Someone) >>",
	}
	data := buildTestPdf(objs, "<< /Root 1 0 R /Info 5 0 R /Size 6 >>", 0, "\n")
	info, _ := readPdfInfoBytes(t, data)
	if info.pages != 2 || info.author != "Someone" {
		t.Errorf("unexpected info %+v", info)
	}
}

func TestPdfInfoIncrementalUpdate(t *testing.T) {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Title (Old) /Author (Kept) >>",
	}
	data := buildTestPdf(objs, "<< /Root 1 0 R /Info 4 0 R /Size 5 >>", 0, "\r\n")
	oldXref := bytes.Index(data, []byte("\nxref\n")) + 1

	var b bytes.Buffer
	b.Write(data)
	newObj := b.Len()
	b.WriteString("4 0 obj\n<< /Title (New) /Author (Kept) >>\nendobj\n")
	newXref := b.Len()
	fmt.Fprintf(&b, "xref\n4 1\n%010d 00000 n\r\n", newObj)
	fmt.Fprintf(&b, "trailer\n<< /Root 1 0 R /Info 4 0 R /Size 5 /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", oldXref, newXref)

	info, _ := readPdfInfoBytes(t, b.Bytes())
	if info.title != "New" || info.author != "Kept" || info.pages != 1 {
		t.Errorf("unexpected info %+v", info)
	}
}

func TestPdfInfoDamaged(t *testing.T) {
	if _, err := readPdfInfo(bytes.NewReader([]byte("hello")), 5); err == nil {
		t.Errorf("expected error for non-PDF data")
	}
	data := []byte("%PDF-1.3\nstartxref\n999999\n%%EOF\n")
	info, err := readPdfInfo(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if info.version != "1.3" || info.pages != -1 {
		t.Errorf("unexpected info %+v", info)
	}
	lines := formatPdfInfoLines(info)
	if len(lines) != 2 || !strings.Contains(lines[1], "unknown") {
		t.Errorf("lines = %q", lines)
	}
}

func TestPdfInfoReferenceLoop(t *testing.T) {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [2 0 R] /Count 3 0 R >>",
		"3 0 R",
	}
	data := buildTestPdf(objs, "<< /Root 1 0 R /Size 4 >>", 0, "\r\n")
	info, _ := readPdfInfoBytes(t, data)
	if info.pages != -1 {
		t.Errorf("pages = %d, want -1", info.pages)
	}
}

func TestDecodePdfText(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte("plain"), "plain"},
		{[]byte{0x93, 'x', 0xE9}, "ﬁxé"},
		{[]byte("\xEF\xBB\xBFa\x1b[31mb\nc\x07"), "a[31mb c"},
		{[]byte{0xFE, 0xFF, 0x00, 'h', 0x00, 0x1B, 0x00, 'e', 0x00, 'n', 0x00, 0x1B, 0x00, 'i'}, "hi"},
		{[]byte{0xEF, 0xBB, 0xBF, 'u', 't', 'f'}, "utf"},
		{[]byte{0xFE, 0xFF, 0x00, 'x', 0x20, 0x2E, 0x00, 'y'}, "xy"},
	}
	for _, c := range cases {
		if got := decodePdfText(c.in); got != c.want {
			t.Errorf("decodePdfText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatPdfDate(t *testing.T) {
	cases := map[string]string{
		"D:20240102030405-05'00'": "2024-01-02 03:04 -05:00",
		"D:20240102030405+0530":   "2024-01-02 03:04 +05:30",
		"D:20240102030405Z00'00'": "2024-01-02 03:04 UTC",
		"D:2024":                  "2024-01-01",
		"20240102":                "2024-01-02",
		"garbage":                 "garbage",
		"":                        "",
	}
	for in, want := range cases {
		if got := formatPdfDate(in); got != want {
			t.Errorf("formatPdfDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatPdfPageSize(t *testing.T) {
	cases := []struct {
		w, h float64
		want string
	}{
		{612, 792, "Letter (8.5 × 11 in, 216 × 279 mm)"},
		{841.89, 595.276, "A4 landscape (11.69 × 8.27 in, 297 × 210 mm)"},
		{3456, 2592, "48 × 36 in, 1219 × 914 mm"},
	}
	for _, c := range cases {
		if got := formatPdfPageSize(c.w, c.h); got != c.want {
			t.Errorf("formatPdfPageSize(%v, %v) = %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

func TestParsePdfValues(t *testing.T) {
	l := &pdfLexer{buf: []byte(`<< /A#20B [1 -2.5 (x\(y\)\101) <48 65 6> true null 7 0 R] /C << >> >>`)}
	v, err := l.parseValue(0)
	if err != nil {
		t.Fatal(err)
	}
	d := v.(pdfDict)
	arr := d["A B"].(pdfArray)
	if arr[0] != int64(1) || arr[1] != -2.5 || string(arr[2].(pdfString)) != "x(y)A" ||
		string(arr[3].(pdfString)) != "He`" || arr[4] != true || arr[5] != nil || arr[6] != (pdfRef{7, 0}) {
		t.Errorf("unexpected array %#v", arr)
	}
	if _, ok := d["C"].(pdfDict); !ok {
		t.Errorf("C = %#v", d["C"])
	}
}

func FuzzReadPdfInfo(f *testing.F) {
	for _, name := range []string{"classic.pdf", "objstm.pdf", "linearized.pdf", "encrypted.pdf"} {
		if data, err := os.ReadFile(filepath.Join("testdata", "pdf", name)); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		info, err := readPdfInfo(bytes.NewReader(data), int64(len(data)))
		if err == nil {
			formatPdfInfoLines(info)
		}
	})
}

func TestPdfInfoManyEmptyXrefSubsections(t *testing.T) {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	xref := b.Len()
	b.WriteString("xref\n")
	for range 50000 {
		b.WriteString("0 0\n")
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 1 >>\nstartxref\n%d\n%%%%EOF\n", xref)
	_, read := readPdfInfoBytes(t, b.Bytes())
	if read > 2*int64(b.Len()) {
		t.Errorf("read %d bytes of a %d byte file", read, b.Len())
	}
}
