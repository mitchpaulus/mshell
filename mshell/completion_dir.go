package main

import (
	"slices"
	"strings"
	"unsafe"
)

// dirEntryKind is what a directory read says about an entry. Links and
// unknown entries need a stat to learn whether they are directories.
type dirEntryKind uint8

const (
	dirKindUnknown dirEntryKind = iota
	dirKindFile
	dirKindDir
	dirKindLink
)

// dirEntryRef locates one name inside DirListing.raw. It is 8 bytes, so a scan
// over a large directory walks one small contiguous array.
type dirEntryRef struct {
	off  uint32
	n    uint8 // Names are at most 255 bytes on every supported filesystem.
	kind dirEntryKind
}

// DirListing is a directory read for completion. Names live back to back in
// raw rather than as one heap object per entry, and the buffers are reused
// across Tab presses. Names handed out by name are only valid until the next
// read.
type DirListing struct {
	raw     []byte
	entries []dirEntryRef
	picked  []int32 // Scratch: the entries one completion pass keeps.
}

// A huge directory should not pin its buffers for the rest of the session.
const dirListingKeepBytes = 1 << 20

func (l *DirListing) reset() {
	if cap(l.raw) > dirListingKeepBytes {
		l.raw = nil
	}
	if cap(l.entries)*int(unsafe.Sizeof(dirEntryRef{})) > dirListingKeepBytes {
		l.entries = nil
	}
	if cap(l.picked)*4 > dirListingKeepBytes {
		l.picked = nil
	}
	l.raw = l.raw[:0]
	l.entries = l.entries[:0]
	l.picked = l.picked[:0]
}

// add appends a name. Readers that already hold the name in raw use addRef.
func (l *DirListing) add(name string, kind dirEntryKind) {
	if name == "" || len(name) > 255 || name == "." || name == ".." {
		return
	}
	off := len(l.raw)
	l.raw = append(l.raw, name...)
	l.entries = append(l.entries, dirEntryRef{off: uint32(off), n: uint8(len(name)), kind: kind})
}

func (l *DirListing) Len() int { return len(l.entries) }

// name views an entry's bytes without copying.
func (l *DirListing) name(i int) string {
	e := l.entries[i]
	return unsafe.String(&l.raw[e.off], int(e.n))
}

// sortPicked orders the picked entries by name, matching os.ReadDir.
func (l *DirListing) sortPicked() {
	slices.SortFunc(l.picked, func(a, b int32) int {
		return strings.Compare(l.name(int(a)), l.name(int(b)))
	})
}
