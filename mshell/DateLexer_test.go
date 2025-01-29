package main

import (
	"testing"
	"fmt"
	"time"
)

func TestDateLexing(t *testing.T) {
	// l := NewDateLexer("2024-01-13")
	l := NewDateLexer("Jun 5, 2027")

	for {
		token := l.scanToken()
		fmt.Println(token)

		if token.Type == DATEEOF {
			fmt.Println("Breaking")
			break
		}
	}
}

func HelpTestDate(dateStr string, expectedYear int, expectedMonth time.Month, expectedDay int, t *testing.T) {
	parsedTime, err := ParseDateTime(dateStr)
	if err != nil {
		t.Fatal(err)
	}

	if parsedTime.Year() != expectedYear || parsedTime.Month() != expectedMonth || parsedTime.Day() != expectedDay {
		t.Errorf("Bad datetime parse")
	}

	fmt.Println(parsedTime)
}

func TestIsoDate(t *testing.T) {
	HelpTestDate("2024-01-11", 2024, time.January, 11, t)
}

func TestDate2(t *testing.T) {
	HelpTestDate("Jun 5, 2027", 2027, time.June, 5, t)
	HelpTestDate("2025 March 3", 2025, time.March, 3, t)
	HelpTestDate("1 august 23", 2023, time.August, 1, t)
	HelpTestDate("24 Sep. 23", 2023, time.September, 24, t)
	HelpTestDate("10/2/2022", 2022, time.October, 2, t)
}
