package main

import (
	"fmt"
	"strconv"
	"strings"
)

// writeCsvCell writes s as one CSV cell. Like toCsvCell, it wraps s in double
// quotes and doubles embedded quotes when s holds a comma, a double quote, or a
// newline.
func writeCsvCell(sb *strings.Builder, s string) {
	if !csvNeedsQuotes(s) {
		sb.WriteString(s)
		return
	}
	sb.WriteByte('"')
	for {
		i := strings.IndexByte(s, '"')
		if i < 0 {
			break
		}
		sb.WriteString(s[:i+1])
		sb.WriteByte('"')
		s = s[i+1:]
	}
	sb.WriteString(s)
	sb.WriteByte('"')
}

// csvNeedsQuotes reports whether s holds a comma, a double quote, or a newline.
// A plain loop is faster here than strings.ContainsAny, which builds a lookup
// table on every call for strings longer than 8 bytes.
func csvNeedsQuotes(s string) bool {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ',', '"', '\n':
			return true
		}
	}
	return false
}

// csvSampleRows is how many rows are written before the output's size is
// estimated from them.
const csvSampleRows = 64

// growForRemainingRows reserves room for the rows not yet written, estimated
// as the average length of the first csvSampleRows rows plus an eighth. Past
// the estimate the builder grows by doubling as usual. The string made from the
// builder keeps all the reserved room, so a sample of unusually long rows
// holds on to the excess.
func growForRemainingRows(sb *strings.Builder, sampleStart int, remaining int) {
	perRow := (sb.Len() - sampleStart) / csvSampleRows
	sb.Grow(perRow * remaining * 9 / 8)
}

// rowsToCsv writes a list of string rows as CSV: cells joined with `,`, each
// row ending in `\n`.
func rowsToCsv(rows *MShellList) (string, error) {
	var sb strings.Builder
	for r, rowObj := range rows.Items {
		if r == csvSampleRows {
			growForRemainingRows(&sb, 0, len(rows.Items)-r)
		}
		row, ok := rowObj.(*MShellList)
		if !ok {
			return "", fmt.Errorf("Expected a list of rows, found a %s (%s).\n", rowObj.TypeName(), rowObj.DebugString())
		}
		for i, cell := range row.Items {
			s, ok := cell.(MShellString)
			if !ok {
				return "", fmt.Errorf("Expected string cells, found a %s (%s).\n", cell.TypeName(), cell.DebugString())
			}
			if i > 0 {
				sb.WriteByte(',')
			}
			writeCsvCell(&sb, s.Content)
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// gridToCsv writes a grid's rows at indices (all rows when indices is nil) as
// CSV, with a header row of the column names first.
func gridToCsv(g *MShellGrid, indices []int) (string, error) {
	var sb strings.Builder
	var num []byte
	for i, col := range g.Columns {
		if i > 0 {
			sb.WriteByte(',')
		}
		writeCsvCell(&sb, col.Name)
	}
	sb.WriteByte('\n')
	headerLen := sb.Len()

	rowCount := g.RowCount
	if indices != nil {
		rowCount = len(indices)
	}
	for r := 0; r < rowCount; r++ {
		if r == csvSampleRows {
			growForRemainingRows(&sb, headerLen, rowCount-r)
		}
		row := r
		if indices != nil {
			row = indices[r]
		}
		for i, col := range g.Columns {
			if i > 0 {
				sb.WriteByte(',')
			}
			switch col.ColType {
			case COL_INT:
				num = strconv.AppendInt(num[:0], col.IntData[row], 10)
				sb.Write(num)
			case COL_FLOAT:
				num = strconv.AppendFloat(num[:0], col.FloatData[row], 'f', -1, 64)
				sb.Write(num)
			case COL_STRING, COL_DICT_STRING:
				writeCsvCell(&sb, col.StringAt(row))
			case COL_DATETIME:
				// Written as `str` writes a date-time, without making one.
				num = appendIso8601(num[:0], col.DateTimeData[row])
				sb.Write(num)
			default:
				if err := writeCsvValue(&sb, col.Get(row)); err != nil {
					return "", fmt.Errorf("Cannot write row %d of column '%s' to CSV. %s", r, col.Name, err.Error())
				}
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// writeCsvValue writes one grid cell as CSV. `none` and null are empty cells,
// `just v` is written as v, and any other value is written in its `str` form.
func writeCsvValue(sb *strings.Builder, obj MShellObject) error {
	if m, ok := asMaybe(obj); ok {
		if m.IsNone() {
			return nil
		}
		return writeCsvValue(sb, m.obj)
	}
	switch v := obj.(type) {
	case MShellString:
		writeCsvCell(sb, v.Content)
	case MShellInt, MShellFloat, MShellBool:
		sb.WriteString(v.ToString())
	case MShellNull:
	default:
		s, err := renderOrError(obj, flavorStr)
		if err != nil {
			return err
		}
		writeCsvCell(sb, s)
	}
	return nil
}
