package main

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

// referenceGroupRows groups rows with one byte-key map lookup per row.
func referenceGroupRows(t *testing.T, grid *MShellGrid, rows []int, keyCols []string) [][]int {
	t.Helper()
	groups := [][]int{}
	seen := map[string]int{}
	for _, row := range rows {
		var key []byte
		for _, name := range keyCols {
			var err error
			key, err = appendGridKeyPart(key, grid.GetColumn(name).Get(row))
			if err != nil {
				t.Fatal(err)
			}
		}
		g, ok := seen[string(key)]
		if !ok {
			g = len(groups)
			seen[string(key)] = g
			groups = append(groups, nil)
		}
		groups[g] = append(groups[g], row)
	}
	return groups
}

func genericTestColumn(name string, values []MShellObject) *GridColumn {
	col := NewGridColumn(name, len(values))
	copy(col.GenericData, values)
	optimizeColumnStorage(col)
	return col
}

func randomTestGrid(rng *rand.Rand, n int) *MShellGrid {
	grid := NewGrid()
	grid.RowCount = n
	short := make([]MShellObject, n)
	long := make([]MShellObject, n)
	distinct := make([]MShellObject, n)
	smallInt := make([]MShellObject, n)
	wideInt := make([]MShellObject, n)
	floats := make([]MShellObject, n)
	dates := make([]MShellObject, n)
	mixed := make([]MShellObject, n)
	specialFloats := []float64{0, math.Copysign(0, -1), math.NaN(), 1.5, -2.25, math.Inf(1)}
	for i := 0; i < n; i++ {
		short[i] = MShellString{Content: fmt.Sprintf("s%d", rng.Intn(20))}
		long[i] = MShellString{Content: fmt.Sprintf("a longer string %d", rng.Intn(15))}
		distinct[i] = MShellString{Content: fmt.Sprintf("d%d", rng.Intn(n*4))}
		smallInt[i] = MShellInt{Value: rng.Intn(40) - 20}
		wideInt[i] = MShellInt{Value: (rng.Intn(10) - 5) * 1_000_000_007}
		floats[i] = MShellFloat{Value: specialFloats[rng.Intn(len(specialFloats))]}
		dates[i] = &MShellDateTime{Time: time.Unix(int64(rng.Intn(5))*86400, 0).UTC()}
		switch rng.Intn(4) {
		case 0:
			mixed[i] = MShellInt{Value: 1}
		case 1:
			mixed[i] = MShellString{Content: "1"}
		case 2:
			mixed[i] = &Maybe{obj: nil}
		default:
			mixed[i] = MShellFloat{Value: 1}
		}
	}
	grid.AddColumn(genericTestColumn("short", short))
	grid.AddColumn(genericTestColumn("long", long))
	grid.AddColumn(genericTestColumn("distinct", distinct))
	grid.AddColumn(genericTestColumn("smallInt", smallInt))
	grid.AddColumn(genericTestColumn("wideInt", wideInt))
	grid.AddColumn(genericTestColumn("float", floats))
	grid.AddColumn(genericTestColumn("date", dates))
	grid.AddColumn(genericTestColumn("mixed", mixed))
	return grid
}

func TestGroupGridRowsMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	n := 5000
	grid := randomTestGrid(rng, n)

	wantTypes := map[string]ColumnType{
		"short":    COL_DICT_STRING,
		"long":     COL_DICT_STRING,
		"distinct": COL_STRING,
		"smallInt": COL_INT,
		"wideInt":  COL_INT,
		"float":    COL_FLOAT,
		"date":     COL_DATETIME,
		"mixed":    COL_GENERIC,
	}
	for name, want := range wantTypes {
		if got := grid.GetColumn(name).ColType; got != want {
			t.Fatalf("column %s: storage %d, want %d", name, got, want)
		}
	}

	allRows := make([]int, n)
	for i := range allRows {
		allRows[i] = i
	}
	var someRows []int
	for i := n - 1; i >= 0; i -= 3 {
		someRows = append(someRows, i)
	}

	keySets := [][]string{
		{},
		{"short"}, {"long"}, {"distinct"}, {"smallInt"}, {"wideInt"}, {"float"}, {"date"}, {"mixed"},
		{"short", "smallInt"},
		{"distinct", "wideInt"},
		{"long", "float", "date"},
		{"mixed", "short", "distinct", "smallInt"},
	}
	for _, rows := range [][]int{allRows, someRows, {}} {
		for _, keys := range keySets {
			got, err := groupGridRows(grid, rows, keys)
			if err != nil {
				t.Fatal(err)
			}
			want := referenceGroupRows(t, grid, rows, keys)
			if len(got) == 0 && len(want) == 0 {
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("keys %v over %d rows: got %d groups, want %d", keys, len(rows), len(got), len(want))
			}
		}
	}
}

func TestGroupGridRowsListsDoNotShareCapacity(t *testing.T) {
	grid := NewGrid()
	grid.RowCount = 4
	grid.AddColumn(genericTestColumn("k", []MShellObject{MShellInt{Value: 1}, MShellInt{Value: 2}, MShellInt{Value: 1}, MShellInt{Value: 2}}))
	groups, err := groupGridRows(grid, []int{0, 1, 2, 3}, []string{"k"})
	if err != nil {
		t.Fatal(err)
	}
	_ = append(groups[0], 99)
	if !reflect.DeepEqual(groups[1], []int{1, 3}) {
		t.Fatalf("appending to one group changed another: %v", groups[1])
	}
}

func TestDictStringColumnFallsBackWhenMostlyDistinct(t *testing.T) {
	values := make([]MShellObject, 4000)
	for i := range values {
		// Sorted input: the first half is all distinct, the rest repeats one value.
		if i < 1500 {
			values[i] = MShellString{Content: fmt.Sprintf("v%d", i)}
		} else {
			values[i] = MShellString{Content: "same"}
		}
	}
	if col := genericTestColumn("c", values); col.ColType != COL_DICT_STRING {
		t.Fatalf("storage %d, want dictionary", col.ColType)
	}

	for i := range values {
		values[i] = MShellString{Content: fmt.Sprintf("v%d", i%2500)}
	}
	col := genericTestColumn("c", values)
	if col.ColType != COL_STRING {
		t.Fatalf("storage %d, want plain strings", col.ColType)
	}
	if got := col.Get(3999).(MShellString).Content; got != "v1499" {
		t.Fatalf("got %q after fallback", got)
	}
}
