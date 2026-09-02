package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const environmentHistoryLimit = 256

type EnvironmentEvent struct {
	DateTime time.Time
	Kind     string
	Source   string
	Changed  bool
}

type EnvironmentHistory struct {
	mutex  sync.Mutex
	events map[string][]EnvironmentEvent
}

func NewEnvironmentHistory() *EnvironmentHistory {
	history := &EnvironmentHistory{events: make(map[string][]EnvironmentEvent)}
	observedAt := time.Now()

	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		history.append(name, EnvironmentEvent{
			DateTime: observedAt,
			Kind:     "inherited",
			Source:   "Inherited from parent",
			Changed:  false,
		})
	}

	return history
}

func canonicalEnvironmentName(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func (history *EnvironmentHistory) append(name string, event EnvironmentEvent) {
	key := canonicalEnvironmentName(name)
	events := append(history.events[key], event)
	if len(events) > environmentHistoryLimit {
		copy(events, events[len(events)-environmentHistoryLimit:])
		events = events[:environmentHistoryLimit]
	}
	history.events[key] = events
}

func (history *EnvironmentHistory) Set(name string, value string, source string) error {
	history.mutex.Lock()
	defer history.mutex.Unlock()

	previous, existed := os.LookupEnv(name)
	if err := os.Setenv(name, value); err != nil {
		return err
	}

	history.append(name, EnvironmentEvent{
		DateTime: time.Now(),
		Kind:     "set",
		Source:   source,
		Changed:  !existed || previous != value,
	})
	return nil
}

func (history *EnvironmentHistory) Unset(name string, source string) error {
	history.mutex.Lock()
	defer history.mutex.Unlock()

	_, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		return err
	}

	history.append(name, EnvironmentEvent{
		DateTime: time.Now(),
		Kind:     "unset",
		Source:   source,
		Changed:  existed,
	})
	return nil
}

func (history *EnvironmentHistory) Inspect(name string) []EnvironmentEvent {
	history.mutex.Lock()
	defer history.mutex.Unlock()

	events := history.events[canonicalEnvironmentName(name)]
	result := make([]EnvironmentEvent, len(events))
	copy(result, events)
	return result
}

func environmentSource(token Token) string {
	if token.TokenFile == nil || token.TokenFile.Path == "" {
		return "input:" + tokenPosition(token)
	}
	return token.TokenFile.Path + ":" + tokenPosition(token)
}

func tokenPosition(token Token) string {
	return strconv.Itoa(token.Line) + ":" + strconv.Itoa(token.Column)
}
