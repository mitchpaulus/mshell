package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// readLimitEnvVar names the environment variable that caps how many bytes a
// bulk read into memory (the `stdin` builtin) will accept before failing.
const readLimitEnvVar = "MSH_READ_LIMIT"

// defaultReadLimit is the cap used when MSH_READ_LIMIT is unset: 100 MiB.
const defaultReadLimit int64 = 100 * 1024 * 1024

// parseReadLimit parses a MSH_READ_LIMIT value: a whole number of bytes.
// A value of 0 removes the limit.
func parseReadLimit(value string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a whole number of bytes, got '%s'", readLimitEnvVar, value)
	}
	return n, nil
}

// readLimit returns the effective bulk-read cap in bytes, or 0 for no limit.
// The environment is consulted on every call so that `setenv` within a script
// takes effect immediately.
func readLimit() (int64, error) {
	value, ok := os.LookupEnv(readLimitEnvVar)
	if !ok {
		return defaultReadLimit, nil
	}
	return parseReadLimit(value)
}

// errReadLimitExceeded is returned by readAllBounded when the input holds more
// bytes than the limit allows.
type errReadLimitExceeded struct {
	limit int64
}

func (e errReadLimitExceeded) Error() string {
	return fmt.Sprintf("input exceeds the %s of %d bytes", readLimitEnvVar, e.limit)
}

// readAllBounded reads r until EOF, failing as soon as more than limit bytes
// have been seen. It reads at most limit+1 bytes, so an overflowing or
// nonterminating stream never grows memory past the cap. A limit of 0 means
// no limit. Input of exactly limit bytes succeeds.
func readAllBounded(r io.Reader, limit int64) ([]byte, error) {
	var buffer bytes.Buffer
	if limit <= 0 {
		_, err := buffer.ReadFrom(r)
		return buffer.Bytes(), err
	}

	n, err := buffer.ReadFrom(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, errReadLimitExceeded{limit}
	}
	return buffer.Bytes(), nil
}
