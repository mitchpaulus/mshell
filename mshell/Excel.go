package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

const (
	xlsxRelTypeWorksheet = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
)

type xlsxWorkbook struct {
	Sheets []xlsxSheetRef `xml:"sheets>sheet"`
}

type xlsxSheetRef struct {
	Name  string `xml:"name,attr"`
	State string `xml:"state,attr"`
	RID   string `xml:"id,attr"`
}

type xlsxRelationships struct {
	Relationship []xlsxRelationship `xml:"Relationship"`
}

type xlsxRelationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
}

type xlsxSST struct {
	SI []xlsxSI `xml:"si"`
}

type xlsxSI struct {
	T string         `xml:"t"`
	R []xlsxRichRun  `xml:"r"`
}

type xlsxRichRun struct {
	T string `xml:"t"`
}

type xlsxWorksheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	R     int     `xml:"r,attr"`
	Cells []xlsxC `xml:"c"`
}

type xlsxC struct {
	R  string         `xml:"r,attr"`
	T  string         `xml:"t,attr"`
	V  *string        `xml:"v"`
	Is *xlsxInlineStr `xml:"is"`
}

type xlsxInlineStr struct {
	T string        `xml:"t"`
	R []xlsxRichRun `xml:"r"`
}

// colRefToIndex converts an A1-style cell reference to a 0-based column index.
// e.g. "A1" -> 0, "Z5" -> 25, "AA10" -> 26.
func colRefToIndex(cellRef string) (int, error) {
	col := 0
	i := 0
	for i < len(cellRef) {
		c := cellRef[i]
		if c >= 'A' && c <= 'Z' {
			col = col*26 + int(c-'A'+1)
			i++
		} else if c >= 'a' && c <= 'z' {
			col = col*26 + int(c-'a'+1)
			i++
		} else {
			break
		}
	}
	if col == 0 {
		return 0, fmt.Errorf("invalid cell reference %q", cellRef)
	}
	return col - 1, nil
}

// sharedStringText concatenates all text runs in a shared string item.
func sharedStringText(si xlsxSI) string {
	if len(si.R) == 0 {
		return si.T
	}
	var sb strings.Builder
	sb.WriteString(si.T)
	for _, run := range si.R {
		sb.WriteString(run.T)
	}
	return sb.String()
}

func inlineStringText(is *xlsxInlineStr) string {
	if is == nil {
		return ""
	}
	if len(is.R) == 0 {
		return is.T
	}
	var sb strings.Builder
	sb.WriteString(is.T)
	for _, run := range is.R {
		sb.WriteString(run.T)
	}
	return sb.String()
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// parseExcelBytes parses the bytes of a .xlsx (OOXML spreadsheet) file and
// returns a dict of sheet name -> list of lists of cell values. Cells are
// returned as MShellFloat for numbers, MShellString for strings (shared,
// inline, or formula-string results), MShellBool for booleans, and a None
// Maybe for error cells. Missing <v> values and padding cells are empty
// strings. Dates are returned as floats (Excel serial dates).
func parseExcelBytes(data []byte) (*MShellDict, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid zip/xlsx file: %w", err)
	}

	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
	}

	workbookFile, ok := files["xl/workbook.xml"]
	if !ok {
		return nil, fmt.Errorf("xl/workbook.xml not found in archive")
	}
	workbookBytes, err := readZipFile(workbookFile)
	if err != nil {
		return nil, fmt.Errorf("reading xl/workbook.xml: %w", err)
	}
	var wb xlsxWorkbook
	if err := xml.Unmarshal(workbookBytes, &wb); err != nil {
		return nil, fmt.Errorf("parsing xl/workbook.xml: %w", err)
	}

	relsFile, ok := files["xl/_rels/workbook.xml.rels"]
	if !ok {
		return nil, fmt.Errorf("xl/_rels/workbook.xml.rels not found in archive")
	}
	relsBytes, err := readZipFile(relsFile)
	if err != nil {
		return nil, fmt.Errorf("reading xl/_rels/workbook.xml.rels: %w", err)
	}
	var rels xlsxRelationships
	if err := xml.Unmarshal(relsBytes, &rels); err != nil {
		return nil, fmt.Errorf("parsing xl/_rels/workbook.xml.rels: %w", err)
	}
	relByID := make(map[string]xlsxRelationship, len(rels.Relationship))
	for _, r := range rels.Relationship {
		relByID[r.ID] = r
	}

	var sharedStrings []string
	if ssFile, ok := files["xl/sharedStrings.xml"]; ok {
		ssBytes, err := readZipFile(ssFile)
		if err != nil {
			return nil, fmt.Errorf("reading xl/sharedStrings.xml: %w", err)
		}
		var sst xlsxSST
		if err := xml.Unmarshal(ssBytes, &sst); err != nil {
			return nil, fmt.Errorf("parsing xl/sharedStrings.xml: %w", err)
		}
		sharedStrings = make([]string, len(sst.SI))
		for i, si := range sst.SI {
			sharedStrings[i] = sharedStringText(si)
		}
	}

	result := NewDict()
	for _, sheet := range wb.Sheets {
		rel, ok := relByID[sheet.RID]
		if !ok {
			return nil, fmt.Errorf("sheet %q references unknown rId %q", sheet.Name, sheet.RID)
		}
		if rel.Type != xlsxRelTypeWorksheet {
			continue
		}

		target := rel.Target
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Join("xl", target)
		}
		target = path.Clean(target)

		sheetFile, ok := files[target]
		if !ok {
			return nil, fmt.Errorf("sheet %q target %q not found in archive", sheet.Name, target)
		}
		sheetBytes, err := readZipFile(sheetFile)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", target, err)
		}

		var ws xlsxWorksheet
		if err := xml.Unmarshal(sheetBytes, &ws); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", target, err)
		}

		rows, err := worksheetToRows(&ws, sharedStrings)
		if err != nil {
			return nil, fmt.Errorf("in sheet %q: %w", sheet.Name, err)
		}
		result.Items[sheet.Name] = rows
	}

	return result, nil
}

func worksheetToRows(ws *xlsxWorksheet, sharedStrings []string) (*MShellList, error) {
	maxRow := 0
	maxCol := -1
	type placedCell struct {
		row  int
		col  int
		cell *xlsxC
	}
	placed := make([]placedCell, 0, 16)

	for i := range ws.Rows {
		row := &ws.Rows[i]
		rowIdx := row.R - 1
		if row.R == 0 {
			rowIdx = i
		}
		if rowIdx+1 > maxRow {
			maxRow = rowIdx + 1
		}
		for j := range row.Cells {
			c := &row.Cells[j]
			var colIdx int
			if c.R == "" {
				colIdx = j
			} else {
				ci, err := colRefToIndex(c.R)
				if err != nil {
					return nil, err
				}
				colIdx = ci
			}
			if colIdx > maxCol {
				maxCol = colIdx
			}
			placed = append(placed, placedCell{row: rowIdx, col: colIdx, cell: c})
		}
	}

	outer := NewList(maxRow)
	for i := 0; i < maxRow; i++ {
		inner := NewList(maxCol + 1)
		for j := 0; j <= maxCol; j++ {
			inner.Items[j] = MShellString{""}
		}
		outer.Items[i] = inner
	}

	for _, p := range placed {
		obj, err := cellToObject(p.cell, sharedStrings)
		if err != nil {
			return nil, err
		}
		inner := outer.Items[p.row].(*MShellList)
		inner.Items[p.col] = obj
	}

	return outer, nil
}

func cellToObject(c *xlsxC, sharedStrings []string) (MShellObject, error) {
	switch c.T {
	case "s":
		if c.V == nil {
			return MShellString{""}, nil
		}
		idx, err := strconv.Atoi(strings.TrimSpace(*c.V))
		if err != nil {
			return nil, fmt.Errorf("shared-string cell %q has non-integer index %q", c.R, *c.V)
		}
		if idx < 0 || idx >= len(sharedStrings) {
			return nil, fmt.Errorf("shared-string cell %q index %d out of range (table has %d entries)", c.R, idx, len(sharedStrings))
		}
		return MShellString{sharedStrings[idx]}, nil
	case "inlineStr":
		return MShellString{inlineStringText(c.Is)}, nil
	case "str":
		if c.V == nil {
			return MShellString{""}, nil
		}
		return MShellString{*c.V}, nil
	case "b":
		if c.V == nil {
			return MShellString{""}, nil
		}
		v := strings.TrimSpace(*c.V)
		return MShellBool{Value: v == "1" || strings.EqualFold(v, "true")}, nil
	case "e":
		return &Maybe{obj: nil}, nil
	case "", "n":
		if c.V == nil {
			return MShellString{""}, nil
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(*c.V), 64)
		if err != nil {
			return nil, fmt.Errorf("numeric cell %q has non-numeric value %q", c.R, *c.V)
		}
		return MShellFloat{f}, nil
	default:
		return nil, fmt.Errorf("cell %q has unsupported type %q", c.R, c.T)
	}
}
