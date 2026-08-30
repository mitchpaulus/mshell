// cursor_query_latency measures terminal DSR/CPR round-trip latency.
//
// Run this through cursor_query_latency.sh in the terminal emulator (and any
// multiplexer or remote connection) that you want to measure. The interval
// between requests is deliberately configurable because a burst of queries
// may exercise different buffering behavior than an occasional shell query.
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
)

const cursorPositionQuery = "\x1b[6n"

type byteResult struct {
	b   byte
	err error
}

type sample struct {
	latency time.Duration
	row     int
	col     int
}

func main() {
	count := flag.Int("count", 500, "number of measured queries")
	warmup := flag.Int("warmup", 20, "number of unmeasured warm-up queries")
	interval := flag.Duration("interval", 0, "delay between query replies and the next query")
	timeout := flag.Duration("timeout", 500*time.Millisecond, "maximum time to wait for each reply")
	raw := flag.Bool("raw", false, "print each measured sample as TSV after the summary")
	flag.Parse()

	if *count < 1 || *warmup < 0 || *interval < 0 || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "count must be positive; warmup and interval non-negative; timeout positive")
		os.Exit(2)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "cursor_query_latency: stdin and stdout must both be the terminal under test")
		os.Exit(2)
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "cursor_query_latency: enter raw mode: %v\n", err)
		os.Exit(1)
	}
	restored := false
	restore := func() {
		if !restored {
			_ = term.Restore(int(os.Stdin.Fd()), oldState)
			restored = true
		}
	}
	defer restore()

	input := make(chan byteResult, 64)
	go readBytes(os.Stdin, input)

	all := make([]sample, 0, *count)
	for i := 0; i < *warmup+*count; i++ {
		if i > 0 && *interval > 0 {
			time.Sleep(*interval)
		}
		start := time.Now()
		if _, err := os.Stdout.WriteString(cursorPositionQuery); err != nil {
			restore()
			fmt.Fprintf(os.Stderr, "cursor_query_latency: write query %d: %v\n", i+1, err)
			os.Exit(1)
		}
		row, col, err := readCursorPosition(input, *timeout)
		elapsed := time.Since(start)
		if err != nil {
			restore()
			fmt.Fprintf(os.Stderr, "cursor_query_latency: query %d: %v\n", i+1, err)
			os.Exit(1)
		}
		if i >= *warmup {
			all = append(all, sample{latency: elapsed, row: row, col: col})
		}
	}

	restore()
	printReport(all, *warmup, *interval, *timeout, *raw)
}

func readBytes(file *os.File, output chan<- byteResult) {
	buf := make([]byte, 64)
	for {
		n, err := file.Read(buf)
		for _, b := range buf[:n] {
			output <- byteResult{b: b}
		}
		if err != nil {
			output <- byteResult{err: err}
			return
		}
	}
}

func readCursorPosition(input <-chan byteResult, timeout time.Duration) (int, int, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var reply []byte
	inReply := false
	for {
		select {
		case result := <-input:
			if result.err != nil {
				return 0, 0, result.err
			}
			if result.b == 3 {
				return 0, 0, errors.New("interrupted")
			}

			if !inReply {
				if result.b == 0x1b {
					reply = append(reply[:0], result.b)
					inReply = true
				}
				continue
			}

			reply = append(reply, result.b)
			if len(reply) == 2 && result.b != '[' {
				inReply = false
				continue
			}
			if result.b == 'R' {
				var row, col int
				if _, err := fmt.Sscanf(string(reply), "\x1b[%d;%dR", &row, &col); err != nil || row < 1 || col < 1 {
					return 0, 0, fmt.Errorf("invalid CPR reply %q", reply)
				}
				return row, col, nil
			}
			if len(reply) > 32 {
				return 0, 0, fmt.Errorf("overlong CPR reply %q", reply)
			}
		case <-timer.C:
			return 0, 0, fmt.Errorf("no CPR reply within %s", timeout)
		}
	}
}

func printReport(samples []sample, warmup int, interval, timeout time.Duration, raw bool) {
	durations := make([]time.Duration, len(samples))
	var total time.Duration
	for i, s := range samples {
		durations[i] = s.latency
		total += s.latency
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	mean := time.Duration(int64(total) / int64(len(durations)))
	var squaredSeconds float64
	for _, d := range durations {
		delta := d.Seconds() - mean.Seconds()
		squaredSeconds += delta * delta
	}
	stddev := time.Duration(math.Sqrt(squaredSeconds/float64(len(durations))) * float64(time.Second))

	fmt.Println("terminal cursor-position query latency")
	fmt.Printf("  query: DSR CPR ESC[6n    samples: %d    warmup: %d\n", len(samples), warmup)
	fmt.Printf("  interval: %s    timeout: %s\n", interval, timeout)
	fmt.Printf("  TERM=%s  TERM_PROGRAM=%s  COLORTERM=%s\n",
		envOrDash("TERM"), envOrDash("TERM_PROGRAM"), envOrDash("COLORTERM"))
	fmt.Printf("  shell=%s  tmux=%s  ssh=%s\n",
		envOrDash("SHELL"), yesNoEnv("TMUX"), yesNoEnv("SSH_CONNECTION"))
	fmt.Println()
	fmt.Printf("  min %9s    p50 %9s    p95 %9s    p99 %9s    max %9s\n",
		compactDuration(durations[0]), compactDuration(percentile(durations, 0.50)),
		compactDuration(percentile(durations, 0.95)), compactDuration(percentile(durations, 0.99)),
		compactDuration(durations[len(durations)-1]))
	fmt.Printf("  mean %8s    stddev %s\n", compactDuration(mean), compactDuration(stddev))

	if raw {
		fmt.Println()
		fmt.Println("sample\tlatency_ns\trow\tcol")
		for i, s := range samples {
			fmt.Printf("%d\t%d\t%d\t%d\n", i+1, s.latency.Nanoseconds(), s.row, s.col)
		}
	}
}

func percentile(sorted []time.Duration, fraction float64) time.Duration {
	index := int(math.Ceil(fraction*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

func compactDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d)/float64(time.Microsecond))
	}
	return fmt.Sprintf("%.3fms", float64(d)/float64(time.Millisecond))
}

func envOrDash(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "-"
	}
	return value
}

func yesNoEnv(name string) string {
	if os.Getenv(name) == "" {
		return "no"
	}
	return "yes"
}
