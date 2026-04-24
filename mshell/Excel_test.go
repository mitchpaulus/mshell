package main

import (
	"archive/zip"
	"bytes"
	"testing"
)

func writeZipFile(t *testing.T, zw *zip.Writer, name, content string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func buildMinimalXlsx(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	writeZipFile(t, zw, "[Content_Types].xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`)

	writeZipFile(t, zw, "xl/workbook.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `+
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
			`<sheets>`+
			`<sheet name="Data" sheetId="1" r:id="rId1"/>`+
			`<sheet name="Summary" sheetId="2" r:id="rId2"/>`+
			`</sheets></workbook>`)

	writeZipFile(t, zw, "xl/_rels/workbook.xml.rels",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>`+
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/>`+
			`</Relationships>`)

	writeZipFile(t, zw, "xl/sharedStrings.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">`+
			`<si><t>hello</t></si>`+
			`<si><t>world</t></si>`+
			`</sst>`)

	// Data sheet:
	// Row 1: "hello" 42       (A1 shared string, B1 number)
	// Row 2: (blank)  3.14 TRUE  (A2 padded, B2 float, C2 bool)
	// Row 3: inline   #DIV/0!  (A3 inline string, B3 error)
	writeZipFile(t, zw, "xl/worksheets/sheet1.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`+
			`<sheetData>`+
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><v>42</v></c></row>`+
			`<row r="2"><c r="B2"><v>3.14</v></c><c r="C2" t="b"><v>1</v></c></row>`+
			`<row r="3"><c r="A3" t="inlineStr"><is><t>inline</t></is></c><c r="B3" t="e"><v>#DIV/0!</v></c></row>`+
			`</sheetData></worksheet>`)

	// Summary sheet: single cell, formula-string result
	writeZipFile(t, zw, "xl/worksheets/sheet2.xml",
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`+
			`<sheetData>`+
			`<row r="1"><c r="A1" t="str"><f>UPPER("ok")</f><v>OK</v></c></row>`+
			`</sheetData></worksheet>`)

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestParseExcelBytes(t *testing.T) {
	data := buildMinimalXlsx(t)
	dict, err := parseExcelBytes(data)
	if err != nil {
		t.Fatalf("parseExcelBytes: %v", err)
	}

	if len(dict.Items) != 2 {
		t.Fatalf("expected 2 sheets, got %d", len(dict.Items))
	}

	dataSheet, ok := dict.Items["Data"].(*MShellList)
	if !ok {
		t.Fatalf("Data sheet missing or wrong type")
	}
	if len(dataSheet.Items) != 3 {
		t.Fatalf("Data sheet: expected 3 rows, got %d", len(dataSheet.Items))
	}

	row1 := dataSheet.Items[0].(*MShellList)
	if len(row1.Items) != 3 {
		t.Fatalf("row1: expected 3 cols (rectangular), got %d", len(row1.Items))
	}
	if s, ok := row1.Items[0].(MShellString); !ok || s.Content != "hello" {
		t.Errorf("A1 shared string: got %v", row1.Items[0])
	}
	if f, ok := row1.Items[1].(MShellFloat); !ok || f.Value != 42 {
		t.Errorf("B1 number: got %v", row1.Items[1])
	}
	if s, ok := row1.Items[2].(MShellString); !ok || s.Content != "" {
		t.Errorf("C1 padding: got %v", row1.Items[2])
	}

	row2 := dataSheet.Items[1].(*MShellList)
	if s, ok := row2.Items[0].(MShellString); !ok || s.Content != "" {
		t.Errorf("A2 padding: got %v", row2.Items[0])
	}
	if f, ok := row2.Items[1].(MShellFloat); !ok || f.Value != 3.14 {
		t.Errorf("B2 float: got %v", row2.Items[1])
	}
	if b, ok := row2.Items[2].(MShellBool); !ok || !b.Value {
		t.Errorf("C2 bool: got %v", row2.Items[2])
	}

	row3 := dataSheet.Items[2].(*MShellList)
	if s, ok := row3.Items[0].(MShellString); !ok || s.Content != "inline" {
		t.Errorf("A3 inlineStr: got %v", row3.Items[0])
	}
	if m, ok := row3.Items[1].(*Maybe); !ok || !m.IsNone() {
		t.Errorf("B3 error cell: expected None Maybe, got %v", row3.Items[1])
	}
	if s, ok := row3.Items[2].(MShellString); !ok || s.Content != "" {
		t.Errorf("C3 padding: got %v", row3.Items[2])
	}

	summary, ok := dict.Items["Summary"].(*MShellList)
	if !ok {
		t.Fatalf("Summary sheet missing")
	}
	if len(summary.Items) != 1 {
		t.Fatalf("Summary: expected 1 row, got %d", len(summary.Items))
	}
	srow := summary.Items[0].(*MShellList)
	if s, ok := srow.Items[0].(MShellString); !ok || s.Content != "OK" {
		t.Errorf("Summary A1 formula-string: got %v", srow.Items[0])
	}
}
