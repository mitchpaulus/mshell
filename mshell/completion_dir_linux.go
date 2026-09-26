package main

import (
	"encoding/binary"
	"io/fs"
	"slices"
	"syscall"
)

// linux_dirent64: ino u64, off i64, reclen u16, type u8, then the name, NUL
// terminated and padded to reclen.
const (
	direntReclenOff = 16
	direntTypeOff   = 18
	direntNameOff   = 19
)

// readDirListing reads dir with getdents64 straight into the listing's buffer.
// The names stay where the kernel put them, between the record headers, so a
// read costs no per-entry allocation.
func readDirListing(dir string, l *DirListing) error {
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	for err == syscall.EINTR {
		fd, err = syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	}
	if err != nil {
		return &fs.PathError{Op: "open", Path: dir, Err: err}
	}
	defer syscall.Close(fd)

	l.reset()
	const chunk = 8192
	for {
		l.raw = slices.Grow(l.raw, chunk)
		start := len(l.raw)
		n, err := syscall.ReadDirent(fd, l.raw[start:cap(l.raw)])
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return &fs.PathError{Op: "readdirent", Path: dir, Err: err}
		}
		if n <= 0 {
			return nil
		}
		l.raw = l.raw[:start+n]
		l.parseDirents(start)
	}
}

func (l *DirListing) parseDirents(start int) {
	buf := l.raw
	for pos := start; pos+direntNameOff <= len(buf); {
		reclen := int(binary.NativeEndian.Uint16(buf[pos+direntReclenOff:]))
		if reclen <= direntNameOff || pos+reclen > len(buf) {
			return
		}
		nameStart := pos + direntNameOff
		n := 0
		for n < reclen-direntNameOff && buf[nameStart+n] != 0 {
			n++
		}
		if n > 0 && n <= 255 && !(n == 1 && buf[nameStart] == '.') && !(n == 2 && buf[nameStart] == '.' && buf[nameStart+1] == '.') {
			kind := dirKindUnknown
			switch buf[pos+direntTypeOff] {
			case syscall.DT_DIR:
				kind = dirKindDir
			case syscall.DT_LNK:
				kind = dirKindLink
			case syscall.DT_UNKNOWN:
				kind = dirKindUnknown
			default:
				kind = dirKindFile
			}
			l.entries = append(l.entries, dirEntryRef{off: uint32(nameStart), n: uint8(n), kind: kind})
		}
		pos += reclen
	}
}
