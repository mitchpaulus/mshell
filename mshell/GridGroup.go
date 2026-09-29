package main

import (
	"math"
	"math/bits"
)

// Grid grouping engine.
//
// Each key column is factorized into dense first-seen codes (0, 1, 2, ...),
// choosing a method by column storage:
//   - int columns with a small value range use a direct array (no hashing),
//   - other fixed-width values use an open-addressing uint64 table,
//   - strings of up to 7 bytes are packed into a uint64 and use the same table,
//   - dictionary-encoded string columns remap their existing codes through an array.
// Codes from multiple key columns are then combined pairwise (a*cardB + b)
// into first-seen group ids. Row lists for every group share one backing array.

// groupCodeTable is an open-addressing hash table from uint64 keys to int32 codes.
type groupCodeTable struct {
	keys  []uint64
	vals  []int32
	mask  uint64
	shift uint
	n     int
}

func newGroupCodeTable(capacity int) *groupCodeTable {
	size := 1 << bits.Len(uint(capacity*2-1))
	t := &groupCodeTable{
		keys:  make([]uint64, size),
		vals:  make([]int32, size),
		mask:  uint64(size - 1),
		shift: uint(64 - bits.TrailingZeros(uint(size))),
	}
	for i := range t.vals {
		t.vals[i] = -1
	}
	return t
}

// getOrInsert returns the code for k, inserting next if k is absent.
func (t *groupCodeTable) getOrInsert(k uint64, next int32) (int32, bool) {
	h := (k * 0x9E3779B97F4A7C15) >> t.shift
	for {
		v := t.vals[h]
		if v < 0 {
			if t.n*2 >= len(t.vals) {
				t.grow()
				return t.getOrInsert(k, next)
			}
			t.keys[h], t.vals[h] = k, next
			t.n++
			return next, true
		}
		if t.keys[h] == k {
			return v, false
		}
		h = (h + 1) & t.mask
	}
}

func (t *groupCodeTable) grow() {
	nt := newGroupCodeTable(len(t.vals))
	for i, v := range t.vals {
		if v < 0 {
			continue
		}
		h := (t.keys[i] * 0x9E3779B97F4A7C15) >> nt.shift
		for nt.vals[h] >= 0 {
			h = (h + 1) & nt.mask
		}
		nt.keys[h], nt.vals[h] = t.keys[i], v
	}
	nt.n = t.n
	*t = *nt
}

// canonicalFloatBits matches numeric equality: -0 groups with 0, and every NaN groups together.
func canonicalFloatBits(f float64) uint64 {
	if f == 0 {
		f = 0
	} else if math.IsNaN(f) {
		f = math.NaN()
	}
	return math.Float64bits(f)
}

// smallIntRange returns the minimum of data at rows, and whether the value range
// is small enough to index a direct array: bounded by 4M entries and twice the row count.
func smallIntRange(data []int64, rows []int) (int64, int, bool) {
	if len(rows) == 0 {
		return 0, 0, false
	}
	lo, hi := data[rows[0]], data[rows[0]]
	for _, row := range rows {
		x := data[row]
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	// hi-lo can overflow for extreme ranges; a negative span means a large range.
	span := hi - lo
	if span < 0 || span >= 1<<22 || uint64(span) > uint64(len(rows))*2 {
		return 0, 0, false
	}
	return lo, int(span) + 1, true
}

// factorizeInts assigns first-seen codes to integer keys, using a direct array
// (a perfect hash) when the value range is small.
func factorizeInts(data []int64, rows []int) ([]int32, int) {
	lo, size, small := smallIntRange(data, rows)
	if !small {
		return factorizeUint64(len(rows), func(p int) uint64 { return uint64(data[rows[p]]) })
	}
	codes := make([]int32, len(rows))
	table := make([]int32, size)
	for i := range table {
		table[i] = -1
	}
	card := int32(0)
	for p, row := range rows {
		slot := &table[data[row]-lo]
		if *slot < 0 {
			*slot = card
			card++
		}
		codes[p] = *slot
	}
	return codes, int(card)
}

// factorizeUint64 assigns first-seen codes to the keys keyAt(0..n-1) with a hash table.
func factorizeUint64(n int, keyAt func(int) uint64) ([]int32, int) {
	codes := make([]int32, n)
	card := int32(0)
	table := newGroupCodeTable(1024)
	for p := 0; p < n; p++ {
		c, inserted := table.getOrInsert(keyAt(p), card)
		if inserted {
			card++
		}
		codes[p] = c
	}
	return codes, int(card)
}

// packShortString packs a string of up to 7 bytes into a uint64, with the length in the top byte.
func packShortString(s string) (uint64, bool) {
	if len(s) > 7 {
		return 0, false
	}
	var k uint64
	for i := 0; i < len(s); i++ {
		k |= uint64(s[i]) << (8 * i)
	}
	return k | uint64(len(s))<<56, true
}

// stringInterner assigns first-seen codes to strings. Short strings are packed
// into integers and hashed as such; longer ones go through a Go map.
type stringInterner struct {
	short *groupCodeTable
	long  map[string]int32
	count int32
}

func newStringInterner() *stringInterner {
	return &stringInterner{short: newGroupCodeTable(1024)}
}

// intern returns the code for s, and whether s was newly added.
func (in *stringInterner) intern(s string) (int32, bool) {
	if k, ok := packShortString(s); ok {
		c, inserted := in.short.getOrInsert(k, in.count)
		if inserted {
			in.count++
		}
		return c, inserted
	}
	if in.long == nil {
		in.long = make(map[string]int32)
	}
	c, ok := in.long[s]
	if !ok {
		c = in.count
		in.long[s] = c
		in.count++
	}
	return c, !ok
}

func factorizeStrings(data []string, rows []int) ([]int32, int) {
	codes := make([]int32, len(rows))
	interner := newStringInterner()
	for p, row := range rows {
		codes[p], _ = interner.intern(data[row])
	}
	return codes, int(interner.count)
}

// factorizeDictCodes renumbers dictionary codes in first-seen order.
// Dictionary entries are distinct, so no string comparison is needed.
func factorizeDictCodes(dictCodes []int32, dictSize int, rows []int) ([]int32, int) {
	codes := make([]int32, len(rows))
	remap := make([]int32, dictSize)
	for i := range remap {
		remap[i] = -1
	}
	card := int32(0)
	for p, row := range rows {
		slot := &remap[dictCodes[row]]
		if *slot < 0 {
			*slot = card
			card++
		}
		codes[p] = *slot
	}
	return codes, int(card)
}

func factorizeGeneric(n int, objAt func(int) MShellObject) ([]int32, int, error) {
	codes := make([]int32, n)
	seen := make(map[string]int32)
	var buf []byte
	var err error
	for p := 0; p < n; p++ {
		buf, err = appendGridKeyPart(buf[:0], objAt(p))
		if err != nil {
			return nil, 0, err
		}
		c, ok := seen[string(buf)]
		if !ok {
			c = int32(len(seen))
			seen[string(buf)] = c
		}
		codes[p] = c
	}
	return codes, len(seen), nil
}

// factorizeGridColumn returns a first-seen code for each of the given rows of col,
// and the number of distinct codes.
func factorizeGridColumn(col *GridColumn, rows []int) ([]int32, int, error) {
	n := len(rows)
	switch col.ColType {
	case COL_INT:
		codes, card := factorizeInts(col.IntData, rows)
		return codes, card, nil
	case COL_FLOAT:
		d := col.FloatData
		codes, card := factorizeUint64(n, func(p int) uint64 { return canonicalFloatBits(d[rows[p]]) })
		return codes, card, nil
	case COL_DATETIME:
		d := col.DateTimeData
		codes, card := factorizeUint64(n, func(p int) uint64 { return uint64(d[rows[p]].UnixNano()) })
		return codes, card, nil
	case COL_STRING:
		codes, card := factorizeStrings(col.StringData, rows)
		return codes, card, nil
	case COL_DICT_STRING:
		codes, card := factorizeDictCodes(col.DictCodes, len(col.DictValues), rows)
		return codes, card, nil
	default:
		d := col.GenericData
		return factorizeGeneric(n, func(p int) MShellObject { return d[rows[p]] })
	}
}

// combineGroupCodes folds two per-row code arrays into first-seen composite ids.
func combineGroupCodes(a []int32, cardA int, b []int32, cardB int) ([]int32, int) {
	n := len(a)
	ids := make([]int32, n)
	card := int32(0)
	product := uint64(cardA) * uint64(cardB)
	// A direct array is bounded by the row count, so it never costs more memory than the codes.
	if product <= uint64(n)*2 || product <= 1<<16 {
		table := make([]int32, product)
		for i := range table {
			table[i] = -1
		}
		width := int32(cardB)
		for p := 0; p < n; p++ {
			slot := &table[a[p]*width+b[p]]
			if *slot < 0 {
				*slot = card
				card++
			}
			ids[p] = *slot
		}
		return ids, int(card)
	}
	table := newGroupCodeTable(1024)
	for p := 0; p < n; p++ {
		c, inserted := table.getOrInsert(uint64(a[p])<<32|uint64(uint32(b[p])), card)
		if inserted {
			card++
		}
		ids[p] = c
	}
	return ids, int(card)
}

// gridGroupIds returns a first-seen group id for each of rows, grouped by the
// values of keyCols, and the number of groups.
func gridGroupIds(sourceGrid *MShellGrid, rows []int, keyCols []string) ([]int32, int, error) {
	if len(keyCols) == 0 {
		if len(rows) == 0 {
			return nil, 0, nil
		}
		return make([]int32, len(rows)), 1, nil
	}
	var ids []int32
	card := 0
	for i, name := range keyCols {
		codes, colCard, err := factorizeGridColumn(sourceGrid.GetColumn(name), rows)
		if err != nil {
			return nil, 0, err
		}
		if i == 0 {
			ids, card = codes, colCard
		} else {
			ids, card = combineGroupCodes(ids, card, codes, colCard)
		}
	}
	return ids, card, nil
}

// groupGridRows partitions rows by the values of keyCols, returning one row list
// per group in first-seen order. Every list is a capacity-limited subslice of one
// shared backing array, so appending to one never overwrites another.
func groupGridRows(sourceGrid *MShellGrid, rows []int, keyCols []string) ([][]int, error) {
	ids, card, err := gridGroupIds(sourceGrid, rows, keyCols)
	if err != nil {
		return nil, err
	}
	offsets := make([]int, card+1)
	for _, id := range ids {
		offsets[id+1]++
	}
	for g := 1; g <= card; g++ {
		offsets[g] += offsets[g-1]
	}
	backing := make([]int, len(rows))
	fill := make([]int, card)
	copy(fill, offsets[:card])
	for p, id := range ids {
		backing[fill[id]] = rows[p]
		fill[id]++
	}
	groups := make([][]int, card)
	for g := 0; g < card; g++ {
		groups[g] = backing[offsets[g]:offsets[g+1]:offsets[g+1]]
	}
	return groups, nil
}
