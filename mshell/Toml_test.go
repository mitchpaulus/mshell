package main

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The toml-test suite, downloaded by get_toml_test.sh. It is not in git, so
// the tests that use it skip when it is missing.
const tomlSuiteDir = "testdata/toml-test"

// tomlSuiteFiles gives the suite's TOML 1.0 test files, such as
// "valid/array/array.toml".
func tomlSuiteFiles() ([]string, error) {
	f, err := os.Open(filepath.Join(tomlSuiteDir, "files-toml-1.0.0"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var files []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if name := scanner.Text(); strings.HasSuffix(name, ".toml") {
			files = append(files, name)
		}
	}
	return files, scanner.Err()
}

func TestTomlSuite(t *testing.T) {
	files, err := tomlSuiteFiles()
	if os.IsNotExist(err) {
		t.Skip("toml-test suite not downloaded; run ./get_toml_test.sh")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(tomlSuiteDir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeToml(string(data))
			if strings.HasPrefix(name, "invalid/") {
				if err == nil {
					t.Fatalf("parsed invalid TOML:\n%s", data)
				}
				return
			}
			if err != nil {
				t.Fatalf("%v\n%s", err, data)
			}

			jsonData, err := os.ReadFile(strings.TrimSuffix(path, ".toml") + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var expected any
			if err := json.Unmarshal(jsonData, &expected); err != nil {
				t.Fatal(err)
			}
			want := tomlTestValue(t, expected)
			if !tomlValuesEqual(got, want) {
				t.Fatalf("got  %s\nwant %s", got.ToJson(), want.ToJson())
			}
		})
	}
}

// tomlTestValue converts toml-test's expected JSON to the value decodeToml
// should give. A value is {"type": ..., "value": ...} with both strings;
// any other object is a table.
func tomlTestValue(t *testing.T, v any) MShellObject {
	switch v := v.(type) {
	case []any:
		list := NewList(len(v))
		for i, item := range v {
			list.Items[i] = tomlTestValue(t, item)
		}
		return list
	case map[string]any:
		typ, isType := v["type"].(string)
		value, isValue := v["value"].(string)
		if len(v) == 2 && isType && isValue {
			return tomlTestScalar(t, typ, value)
		}
		dict := NewDict()
		for key, item := range v {
			dict.Items[key] = tomlTestValue(t, item)
		}
		return dict
	}
	t.Fatalf("unexpected JSON value %v", v)
	return nil
}

func tomlTestScalar(t *testing.T, typ, value string) MShellObject {
	var obj MShellObject
	var err error
	switch typ {
	case "string":
		obj = MShellString{value}
	case "bool":
		obj = MShellBool{value == "true"}
	case "integer":
		var n int64
		n, err = strconv.ParseInt(value, 10, 64)
		obj = MShellInt{int(n)}
	case "float":
		var f float64
		switch value {
		case "nan", "+nan", "-nan":
			f = math.NaN()
		case "inf", "+inf":
			f = math.Inf(1)
		case "-inf":
			f = math.Inf(-1)
		default:
			f, err = strconv.ParseFloat(value, 64)
		}
		obj = MShellFloat{f}
	case "datetime":
		var tm time.Time
		tm, err = time.Parse(time.RFC3339Nano, value)
		obj = &MShellDateTime{Time: tm.UTC()}
	case "datetime-local":
		var tm time.Time
		tm, err = time.Parse("2006-01-02T15:04:05.999999999", value)
		obj = &MShellDateTime{Time: tm}
	case "date-local":
		var tm time.Time
		tm, err = time.Parse("2006-01-02", value)
		obj = &MShellDateTime{Time: tm}
	case "time-local":
		var tm time.Time
		tm, err = time.Parse("15:04:05.999999999", value)
		obj = MShellString{tm.Format("15:04:05.999999999")}
	default:
		t.Fatalf("unknown toml-test type %q", typ)
	}
	if err != nil {
		t.Fatalf("bad toml-test %s %q: %v", typ, value, err)
	}
	return obj
}

// tomlValuesEqual compares exactly: NaN equals NaN, 0.0 and -0.0 differ, and
// datetimes must match in time zone as well as instant.
func tomlValuesEqual(a, b MShellObject) bool {
	switch x := a.(type) {
	case *MShellDict:
		y, ok := b.(*MShellDict)
		if !ok || len(x.Items) != len(y.Items) {
			return false
		}
		for key, v := range x.Items {
			w, ok := y.Items[key]
			if !ok || !tomlValuesEqual(v, w) {
				return false
			}
		}
		return true
	case *MShellList:
		y, ok := b.(*MShellList)
		if !ok || len(x.Items) != len(y.Items) {
			return false
		}
		for i := range x.Items {
			if !tomlValuesEqual(x.Items[i], y.Items[i]) {
				return false
			}
		}
		return true
	case MShellFloat:
		y, ok := b.(MShellFloat)
		if !ok {
			return false
		}
		if math.IsNaN(x.Value) || math.IsNaN(y.Value) {
			return math.IsNaN(x.Value) && math.IsNaN(y.Value)
		}
		return x.Value == y.Value && math.Signbit(x.Value) == math.Signbit(y.Value)
	case *MShellDateTime:
		y, ok := b.(*MShellDateTime)
		return ok && x.Time.Equal(y.Time) && x.Time.Location() == y.Time.Location()
	}
	return a == b
}

// FuzzDecodeToml checks that no input crashes the parser, and that any input
// it accepts gives a dict whose keys and strings are valid UTF-8. The seeds
// include the toml-test files when the suite has been downloaded.
func FuzzDecodeToml(f *testing.F) {
	for _, seed := range []string{
		"",
		"a = 1\nb = \"two\"\nc = 3.0\nd = true",
		"[t]\nx.y = 'lit'\n[[arr]]\nk = [1, [2, 3], {z = 1979-05-27T07:32:00Z}]\n[[arr]]",
		"s = \"\"\"\nline \\\n  next \\u00e9\"\"\"\nl = '''\n''raw'''",
		"n = [0x1f, 0o17, 0b1, 1_000, -0.5e-3, inf, nan]\nd = 1979-05-27 07:32:00.123\nt = 07:32:00",
	} {
		f.Add(seed)
	}
	if files, err := tomlSuiteFiles(); err == nil {
		for _, name := range files {
			if data, err := os.ReadFile(filepath.Join(tomlSuiteDir, name)); err == nil {
				f.Add(string(data))
			}
		}
	}

	f.Fuzz(func(t *testing.T, s string) {
		dict, err := decodeToml(s)
		if err != nil {
			return
		}
		if dict == nil {
			t.Fatal("no error and no dict")
		}
		tomlCheckUTF8(t, dict)
	})
}

func tomlCheckUTF8(t *testing.T, obj MShellObject) {
	switch o := obj.(type) {
	case *MShellDict:
		for key, v := range o.Items {
			if !utf8.ValidString(key) {
				t.Fatalf("key %q is not valid UTF-8", key)
			}
			tomlCheckUTF8(t, v)
		}
	case *MShellList:
		for _, v := range o.Items {
			tomlCheckUTF8(t, v)
		}
	case MShellString:
		if !utf8.ValidString(o.Content) {
			t.Fatalf("string %q is not valid UTF-8", o.Content)
		}
	}
}

// FuzzTomlTextEnd checks that tomlTextEnd, which reads 8 bytes at a time,
// stops at the same byte as a byte-by-byte loop, from every start index.
func FuzzTomlTextEnd(f *testing.F) {
	for _, seed := range []string{
		"",
		"plain text with no stops at all, long enough for several words",
		"0123456\"89abcdef\\",
		"tab\there, del\x7f, nul\x00, quote' and é \xff\xfe bytes",
		"\x1f\x20\x21\x22\x5b\x5c\x5d\x7e\x7f\x80\x9f\xa0\xdc\xe2",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, stop := range [][2]byte{{'"', '\\'}, {'\'', '\''}, {0, 0}} {
			a, b := stop[0], stop[1]
			for i := 0; i <= len(s); i++ {
				want := i
				for want < len(s) && s[want] != a && s[want] != b && s[want] >= 0x20 && s[want] != 0x7f {
					want++
				}
				if got := tomlTextEnd(s, i, a, b); got != want {
					t.Fatalf("tomlTextEnd(%q, %d, %q, %q) = %d, want %d", s, i, a, b, got, want)
				}
			}
		}
	})
}
