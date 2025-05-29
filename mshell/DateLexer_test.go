package main

import (
	"testing"
	"fmt"
	"time"
	"os"
)

func TestDateLexing(t *testing.T) {
	// l := NewDateLexer("2024-01-13")
	l := NewDateLexer("1/12/2025 12:56:13 AM")

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

func HelpTestDateTime(dateStr string, expectedYear int, expectedMonth time.Month, expectedDay int, expectedHour int, expectedMinute int, expectedSecond int, t *testing.T) {
	parsedTime, err := ParseDateTime(dateStr)
	if err != nil {
		t.Fatal(err)
	}

	if parsedTime.Year() != expectedYear {
		t.Errorf("Parsed year %d, expected %d", parsedTime.Year(), expectedYear)
	}

	if parsedTime.Month() != expectedMonth {
		t.Errorf("Parsed month %d, expected %d", parsedTime.Month(), expectedMonth)
	}

	if parsedTime.Day() != expectedDay {
		t.Errorf("Parsed day %d, expected %d", parsedTime.Day(), expectedDay)
	}

	if parsedTime.Hour() != expectedHour {
		t.Errorf("%s: Parsed hour %d, expected %d", dateStr, parsedTime.Hour(), expectedHour)
	}

	if parsedTime.Minute() != expectedMinute {
		t.Errorf("Parsed minute %d, expected %d", parsedTime.Minute(), expectedMinute)
	}

	if parsedTime.Second() != expectedSecond {
		t.Errorf("Parsed second %d, expected %d", parsedTime.Second(), expectedSecond)
	}

	fmt.Fprintf(os.Stderr, "'%s' = %s\n", dateStr, parsedTime)
	// fmt.Println(parsedTime)
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

	HelpTestDateTime("1/12/2025 12:56:13 AM", 2025, time.January, 12, 0, 56, 13, t)
	HelpTestDateTime("1/12/2025 12:56:13 PM", 2025, time.January, 12, 12, 56, 13, t)
	HelpTestDateTime("1/12/2025 2:56:13 PM", 2025, time.January, 12, 14, 56, 13, t)
}

func TestIsoDateFromMsGraph(t *testing.T) {
	// This is the format used by MS Graph API
	HelpTestDateTime("2025-04-30T17:58:18.5467067Z", 2025, time.April, 30, 17, 58, 18, t)
}
