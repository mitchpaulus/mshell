// Prototype of the history log lock protocol. Throwaway: it exists to test
// the design in ai/history-storage.typ, not to be merged.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const headerLen = 16

var (
	castagnoli = crc32.MakeTable(crc32.Castagnoli)
	errBusy    = errors.New("lock busy")
	errNoLocks = errors.New("locks not supported here")
)

func logPath(dir string) string  { return filepath.Join(dir, "msh_history.v2") }
func lockPath(dir string) string { return filepath.Join(dir, "msh_history.v2.lock") }
func tmpPath(dir string) string  { return filepath.Join(dir, "msh_history.v2.tmp") }
func donePath(dir string) string { return filepath.Join(dir, "done") }

// retry repeats f while it fails with an error that may clear by waiting
// (another program briefly has the file open on Windows).
func retry(budget time.Duration, f func() error) (err error, tries int) {
	deadline := time.Now().Add(budget)
	sleep := time.Millisecond
	for {
		tries++
		err = f()
		if err == nil || !isEphemeral(err) || time.Now().After(deadline) {
			return err, tries
		}
		time.Sleep(sleep + time.Duration(rand.Int63n(int64(sleep))))
		if sleep < 32*time.Millisecond {
			sleep *= 2
		}
	}
}

// withLock polls for the lock until the budget runs out, runs f, and unlocks
// explicitly before closing.
func withLock(dir string, exclusive bool, budget time.Duration, f func() error) error {
	if os.Getenv("LOCKPROTO_NOLOCK") != "" {
		return f() // the no-lock design: append mode and the CRC are the only protection
	}
	deadline := time.Now().Add(budget)
	sleep := time.Millisecond
	for {
		lf, err := os.OpenFile(lockPath(dir), os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		err = tryLock(lf, exclusive)
		if err == nil {
			if !sameFile(lf, lockPath(dir)) {
				unlock(lf)
				lf.Close()
				continue
			}
			ferr := f()
			uerr := unlock(lf)
			lf.Close()
			if ferr != nil {
				return ferr
			}
			return uerr
		}
		lf.Close()
		if !errors.Is(err, errBusy) {
			return err
		}
		if time.Now().After(deadline) {
			return errBusy
		}
		time.Sleep(sleep + time.Duration(rand.Int63n(int64(sleep))))
		if sleep < 16*time.Millisecond {
			sleep *= 2
		}
	}
}

func appendHeader(buf []byte, generation uint64) []byte {
	buf = append(buf, 'M', 'S', 'H', 'H')
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint16(buf, 0)
	return binary.LittleEndian.AppendUint64(buf, generation)
}

// parseHeader returns the generation, or ok false for a missing or damaged header.
func parseHeader(b []byte) (generation uint64, ok bool) {
	if len(b) < headerLen || string(b[:4]) != "MSHH" {
		return 0, false
	}
	return binary.LittleEndian.Uint64(b[8:16]), true
}

func appendFrame(buf []byte, kind byte, payload []byte) []byte {
	start := len(buf)
	buf = append(buf, 0xFF, 0xFE, kind)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(payload)))
	buf = append(buf, payload...)
	return binary.LittleEndian.AppendUint32(buf, crc32.Checksum(buf[start+2:], castagnoli))
}

// frameAt returns the length of a whole, valid frame at the start of b.
func frameAt(b []byte) (int, bool) {
	if len(b) < 11 || b[0] != 0xFF || b[1] != 0xFE {
		return 0, false
	}
	n := int(binary.LittleEndian.Uint32(b[3:7]))
	if n > 1<<20 || len(b) < 11+n {
		return 0, false
	}
	if crc32.Checksum(b[2:7+n], castagnoli) != binary.LittleEndian.Uint32(b[7+n:11+n]) {
		return 0, false
	}
	return 11 + n, true
}

// parse emits every valid frame. It never advances past bytes it could not
// parse unless a later frame parses, so an unfinished tail is retried later.
func parse(data []byte, emit func(kind byte, payload []byte)) (consumed int, skipped int) {
	pos := 0
	for pos < len(data) {
		if n, ok := frameAt(data[pos:]); ok {
			emit(data[pos+2], data[pos+7:pos+n-4])
			pos += n
			continue
		}
		next := -1
		for q := pos + 1; q+11 <= len(data); q++ {
			if data[q] == 0xFF && data[q+1] == 0xFE {
				if _, ok := frameAt(data[q:]); ok {
					next = q
					break
				}
			}
		}
		if next < 0 {
			break
		}
		skipped += next - pos
		pos = next
	}
	return pos, skipped
}

func openRetry(path string, flag int) (*os.File, error) {
	var f *os.File
	err, _ := retry(2*time.Second, func() (e error) { f, e = os.OpenFile(path, flag, 0600); return })
	return f, err
}

// readHeader uses its own read-only handle, so the append handle carries no read access.
func readHeader(dir string) (generation uint64, ok bool, err error) {
	f, err := openRetry(logPath(dir), os.O_RDONLY)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	var b [headerLen]byte
	n, _ := io.ReadFull(f, b[:])
	generation, ok = parseHeader(b[:n])
	return generation, ok, nil
}

func appendRecord(dir string, payload []byte) error {
	return withLock(dir, true, 10*time.Second, func() error {
		var buf []byte
		st, err := os.Stat(logPath(dir))
		if os.IsNotExist(err) || (err == nil && st.Size() == 0) {
			buf = appendHeader(buf, rand.Uint64())
		} else if err != nil {
			return err
		} else if _, _, err := readHeader(dir); err != nil {
			return err
		}
		buf = appendFrame(buf, 2, payload)
		f, err := openRetry(logPath(dir), os.O_WRONLY|os.O_APPEND|os.O_CREATE)
		if err != nil {
			return err
		}
		n, werr := f.Write(buf)
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if n != len(buf) {
			return io.ErrShortWrite
		}
		return cerr
	})
}

var renameTries, renameRetried int

func rewrite(dir string) error {
	return withLock(dir, true, 10*time.Second, func() error {
		f, err := openRetry(logPath(dir), os.O_RDONLY)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return err
		}
		start := 0
		if _, ok := parseHeader(data); ok {
			start = headerLen
		}
		out := appendHeader(nil, rand.Uint64())
		parse(data[start:], func(kind byte, payload []byte) { out = appendFrame(out, kind, payload) })
		tf, err := openRetry(tmpPath(dir), os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			return err
		}
		if _, err := tf.Write(out); err != nil {
			tf.Close()
			return err
		}
		if err := tf.Sync(); err != nil {
			tf.Close()
			return err
		}
		if err := tf.Close(); err != nil {
			return err
		}
		err, tries := retry(2*time.Second, func() error { return os.Rename(tmpPath(dir), logPath(dir)) })
		renameTries++
		if tries > 1 {
			renameRetried++
		}
		return err
	})
}

type reader struct {
	dir        string
	generation uint64
	off        int64
	size       int64
	mtime      time.Time
	seen       map[[2]uint32]int
	last       map[uint32]int64
	reloads    int
	tails      int
	busy       int
	disorder   int
}

func (r *reader) emit(kind byte, p []byte) {
	if kind != 2 || len(p) < 8 {
		return
	}
	id, seq := binary.LittleEndian.Uint32(p[0:4]), binary.LittleEndian.Uint32(p[4:8])
	r.seen[[2]uint32{id, seq}]++
	if prev, ok := r.last[id]; ok && int64(seq) != prev+1 && int(id) != victim() {
		r.disorder++ // the victim's run has gaps where records were blanked
	}
	r.last[id] = int64(seq)
}

func (r *reader) refresh(budget time.Duration) error {
	st, err := os.Stat(logPath(r.dir))
	if err != nil {
		if os.IsNotExist(err) || isEphemeral(err) {
			return nil
		}
		return err
	}
	if st.Size() == r.size && st.ModTime().Equal(r.mtime) {
		return nil
	}
	err = withLock(r.dir, false, budget, func() error {
		f, err := openRetry(logPath(r.dir), os.O_RDONLY)
		if err != nil {
			return err
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		var hb [headerLen]byte
		n, _ := io.ReadFull(f, hb[:])
		generation, ok := parseHeader(hb[:n])
		first := int64(0)
		if ok {
			first = headerLen
		}
		if r.off == 0 || generation != r.generation || fi.Size() < r.off {
			if r.off != 0 {
				r.reloads++
			}
			r.seen, r.last = map[[2]uint32]int{}, map[uint32]int64{}
			r.generation, r.off = generation, first
		} else {
			r.tails++
		}
		data := make([]byte, fi.Size()-r.off)
		if _, err := f.ReadAt(data, r.off); err != nil && err != io.EOF {
			return err
		}
		consumed, _ := parse(data, r.emit)
		r.off += int64(consumed)
		r.size, r.mtime = fi.Size(), fi.ModTime()
		return nil
	})
	if errors.Is(err, errBusy) {
		r.busy++
		return nil
	}
	return err
}

// victim is the appender whose records the eraser blanks, or -1.
func victim() int {
	if v := os.Getenv("LOCKPROTO_ERASE"); v != "" {
		return atoi(v)
	}
	return -1
}

func check(seen map[[2]uint32]int, procs, n int) error {
	for id := 0; id < procs; id++ {
		for seq := 0; seq < n; seq++ {
			c := seen[[2]uint32{uint32(id), uint32(seq)}]
			if id == victim() && c <= 1 {
				continue // an erased record may be gone, but never doubled
			}
			if c != 1 {
				return fmt.Errorf("record %d/%d seen %d times", id, seq, c)
			}
		}
	}
	if victim() < 0 && len(seen) != procs*n {
		return fmt.Errorf("%d distinct records, want %d", len(seen), procs*n)
	}
	return nil
}

// eraseOnce blanks every whole frame of the victim in place. The file is
// never replaced and never changes length here.
func eraseOnce(dir string, id uint32) (int, error) {
	data, err := os.ReadFile(logPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	f, err := openRetry(logPath(dir), os.O_RDWR)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	erased := 0
	for pos := 0; pos+11 <= len(data); {
		n, ok := frameAt(data[pos:])
		if !ok {
			pos++
			continue
		}
		if p := data[pos+7 : pos+n-4]; data[pos+2] == 2 && len(p) >= 8 && binary.LittleEndian.Uint32(p[0:4]) == id {
			if _, err := f.WriteAt(make([]byte, n), int64(pos)); err != nil {
				return erased, err
			}
			erased++
		}
		pos += n
	}
	return erased, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: lockproto run DIR APPENDERS N READERS REWRITES INTRUDERS")
		os.Exit(2)
	}
	mode, dir := os.Args[1], os.Args[2]
	fail := func(err error) {
		fmt.Printf("%s FAILED: %v\n", mode, err)
		os.Exit(1)
	}
	done := func() bool { _, err := os.Stat(donePath(dir)); return err == nil }
	switch mode {
	case "append":
		id, n := atoi(os.Args[3]), atoi(os.Args[4])
		rnd := rand.New(rand.NewSource(int64(id) + 1))
		for seq := 0; seq < n; seq++ {
			p := make([]byte, 8+rnd.Intn(300))
			binary.LittleEndian.PutUint32(p[0:4], uint32(id))
			binary.LittleEndian.PutUint32(p[4:8], uint32(seq))
			for i := 8; i < len(p); i++ {
				p[i] = byte(rnd.Intn(256)) // includes FF FE pairs, to exercise resync
			}
			if err := appendRecord(dir, p); err != nil {
				fail(err)
			}
		}
	case "rewrite":
		for i, n := 0, atoi(os.Args[3]); i < n; i++ {
			if err := rewrite(dir); err != nil {
				fail(err)
			}
			time.Sleep(time.Duration(rand.Intn(4)) * time.Millisecond)
		}
		fmt.Printf("rewriter: %d renames, %d needed a retry\n", renameTries, renameRetried)
	case "read":
		procs, n := atoi(os.Args[3]), atoi(os.Args[4])
		r := &reader{dir: dir, seen: map[[2]uint32]int{}, last: map[uint32]int64{}}
		for !done() {
			if err := r.refresh(0); err != nil { // one try: a prompt never waits
				fail(err)
			}
			time.Sleep(time.Millisecond)
		}
		r.size = -1
		if err := r.refresh(10 * time.Second); err != nil {
			fail(err)
		}
		if err := check(r.seen, procs, n); err != nil {
			fail(err)
		}
		if r.disorder != 0 {
			fail(fmt.Errorf("%d records out of order within a writer", r.disorder))
		}
		fmt.Printf("reader: %d tail reads, %d full reloads, %d skipped because the lock was busy\n", r.tails, r.reloads, r.busy)
	case "erase":
		total := 0
		for !done() {
			n, err := eraseOnce(dir, uint32(atoi(os.Args[3])))
			if err != nil {
				fail(err)
			}
			total += n
			time.Sleep(2 * time.Millisecond)
		}
		fmt.Printf("eraser: blanked %d records in place\n", total)
	case "intrude":
		// Stands in for an antivirus or indexer: opens the log with no lock and holds it.
		opens := 0
		for !done() {
			if f, err := os.Open(logPath(dir)); err == nil {
				opens++
				time.Sleep(3 * time.Millisecond)
				f.Close()
			}
			time.Sleep(time.Millisecond)
		}
		fmt.Printf("intruder: held the log open %d times\n", opens)
	case "run":
		appenders, n, readers, rewrites, intruders := atoi(os.Args[3]), atoi(os.Args[4]), atoi(os.Args[5]), atoi(os.Args[6]), atoi(os.Args[7])
		spawn := func(args ...string) *exec.Cmd {
			c := exec.Command(os.Args[0], args...)
			c.Stdout, c.Stderr = os.Stdout, os.Stderr
			if err := c.Start(); err != nil {
				fail(err)
			}
			return c
		}
		start := time.Now()
		var work, watch []*exec.Cmd
		for i := 0; i < readers; i++ {
			watch = append(watch, spawn("read", dir, strconv.Itoa(appenders), strconv.Itoa(n)))
		}
		for i := 0; i < intruders; i++ {
			watch = append(watch, spawn("intrude", dir))
		}
		if victim() >= 0 {
			watch = append(watch, spawn("erase", dir, strconv.Itoa(victim())))
		}
		for i := 0; i < appenders; i++ {
			work = append(work, spawn("append", dir, strconv.Itoa(i), strconv.Itoa(n)))
		}
		if rewrites > 0 {
			work = append(work, spawn("rewrite", dir, strconv.Itoa(rewrites)))
		}
		bad := false
		for _, c := range work {
			bad = c.Wait() != nil || bad
		}
		os.WriteFile(donePath(dir), nil, 0600)
		for _, c := range watch {
			bad = c.Wait() != nil || bad
		}
		data, err := os.ReadFile(logPath(dir))
		if err != nil {
			fail(err)
		}
		seen := map[[2]uint32]int{}
		_, skipped := parse(data[headerLen:], func(kind byte, p []byte) {
			seen[[2]uint32{binary.LittleEndian.Uint32(p[0:4]), binary.LittleEndian.Uint32(p[4:8])}]++
		})
		if err := check(seen, appenders, n); err != nil {
			fail(err)
		}
		if bad {
			fail(errors.New("a child process failed"))
		}
		fmt.Printf("OK: %d of %d records from %d appenders, none doubled or damaged; %d bytes skipped; %s\n", len(seen), appenders*n, appenders, skipped, time.Since(start).Round(time.Millisecond))
	}
}
