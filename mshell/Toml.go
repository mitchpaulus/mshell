package main

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// decodeToml parses a TOML document. A document is a table, so parseToml
// always gives a dict.
func decodeToml(data string) (map[string]any, error) {
	if !utf8.ValidString(data) {
		return nil, fmt.Errorf("input is not valid UTF-8")
	}
	var v map[string]any
	if _, err := toml.Decode(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// ParseTomlObjToMshell converts a decoded TOML value. Datetimes become
// datetimes: one with an offset is converted to UTC, and a local one (no
// offset) keeps its clock time. A local date is a datetime at midnight. A
// local time has no date, so it is a string, such as "07:32:00".
func ParseTomlObjToMshell(tomlObj any) MShellObject {
	// See https://pkg.go.dev/github.com/BurntSushi/toml#hdr-Decoding
	switch o := tomlObj.(type) {
	case map[string]any:
		dict := NewDict()
		for key, value := range o {
			dict.Items[key] = ParseTomlObjToMshell(value)
		}
		return dict
	case []map[string]any:
		// An array of tables (`[[name]]`).
		list := NewList(0)
		for _, item := range o {
			list.Items = append(list.Items, ParseTomlObjToMshell(item))
		}
		return list
	case []any:
		list := NewList(0)
		for _, item := range o {
			list.Items = append(list.Items, ParseTomlObjToMshell(item))
		}
		return list
	case string:
		return MShellString{o}
	case int64:
		return MShellInt{int(o)}
	case float64:
		return MShellFloat{o}
	case bool:
		return MShellBool{o}
	case time.Time:
		switch o.Location().String() {
		case "time-local":
			return MShellString{o.Format("15:04:05.999999999")}
		case "datetime-local", "date-local":
			wall := time.Date(o.Year(), o.Month(), o.Day(), o.Hour(), o.Minute(), o.Second(), o.Nanosecond(), time.UTC)
			return &MShellDateTime{Time: wall}
		default:
			return &MShellDateTime{Time: o.UTC()}
		}
	default:
		panic(fmt.Sprintf("Unknown TOML object type: %T", tomlObj))
	}
}
