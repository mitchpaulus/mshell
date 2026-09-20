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
	HelpTestDateOrder(dateStr, new(DateOrder), expectedYear, expectedMonth, expectedDay, t)
}

func HelpTestDateOrder(dateStr string, order *DateOrder, expectedYear int, expectedMonth time.Month, expectedDay int, t *testing.T) {
	parsedTime, err := ParseDateTime(dateStr, order)
	if err != nil {
		t.Fatal(err)
	}

	if parsedTime.Year() != expectedYear || parsedTime.Month() != expectedMonth || parsedTime.Day() != expectedDay {
		t.Errorf("Bad datetime parse")
	}

	fmt.Println(parsedTime)
}

func HelpTestDateTime(dateStr string, expectedYear int, expectedMonth time.Month, expectedDay int, expectedHour int, expectedMinute int, expectedSecond int, t *testing.T) {
	HelpTestDateTimeOrder(dateStr, new(DateOrder), expectedYear, expectedMonth, expectedDay, expectedHour, expectedMinute, expectedSecond, t)
}

func HelpTestDateTimeOrder(dateStr string, order *DateOrder, expectedYear int, expectedMonth time.Month, expectedDay int, expectedHour int, expectedMinute int, expectedSecond int, t *testing.T) {
	parsedTime, err := ParseDateTime(dateStr, order)
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
	// 24 Sep 2023 or 23 Sep 2024: ambiguous with no prior evidence.
	if _, err := ParseDateTime("24 Sep. 23", new(DateOrder)); err == nil {
		t.Errorf("Expected '24 Sep. 23' to be ambiguous")
	}
	// 10/2/2022 is ambiguous (Oct 2 or 10 Feb) until an order is established.
	if _, err := ParseDateTime("10/2/2022", new(DateOrder)); err == nil {
		t.Errorf("Expected '10/2/2022' to be ambiguous")
	}
	mdy := new(DateOrder)
	*mdy = ORDER_MDY
	HelpTestDateOrder("10/2/2022", mdy, 2022, time.October, 2, t)
	HelpTestDate("12/16/25", 2025, time.December, 16, t)

	for _, bad := range []string{"13/45/2025", "2025-16-12", "2/30/2025", "12/16/2025 25:00", "12/16/2025 10:61"} {
		if _, err := ParseDateTime(bad, new(DateOrder)); err == nil {
			t.Errorf("Expected '%s' to fail to parse", bad)
		}
	}
	HelpTestDateOrder("1/2/25", mdy, 2025, time.January, 2, t)
	// Unambiguous readings in other orders are accepted.
	HelpTestDate("16/06/2025", 2025, time.June, 16, t)
	HelpTestDate("45-06-16", 2045, time.June, 16, t)
	HelpTestDate("16 Sep 5", 2016, time.September, 5, t)
	// Two distinct valid readings with no convention stay unparsed.
	if _, err := ParseDateTime("16 5 06", new(DateOrder)); err == nil {
		t.Errorf("Expected '16 5 06' to be ambiguous")
	}
	HelpTestDateTime("12/16/25 2:56 PM", 2025, time.December, 16, 14, 56, 0, t)

	HelpTestDateTimeOrder("1/12/2025 12:56:13 AM", mdy, 2025, time.January, 12, 0, 56, 13, t)
	HelpTestDateTimeOrder("1/12/2025 12:56:13 PM", mdy, 2025, time.January, 12, 12, 56, 13, t)
	HelpTestDateTimeOrder("1/12/2025 2:56:13 PM", mdy, 2025, time.January, 12, 14, 56, 13, t)
}

func TestIsoDateFromMsGraph(t *testing.T) {
	// This is the format used by MS Graph API
	HelpTestDateTime("2025-04-30T17:58:18.5467067Z", 2025, time.April, 30, 17, 58, 18, t)
}

func TestDateOrderLearning(t *testing.T) {
	// Ambiguous with nothing learned: fails.
	order := new(DateOrder)
	if _, err := ParseDateTime("06/05/2025", order); err == nil {
		t.Errorf("Expected ambiguous date to fail with no learned order")
	}

	// ISO does not teach anything.
	if _, err := ParseDateTime("2025-06-16", order); err != nil {
		t.Errorf("ISO failed: %v", err)
	}
	if *order != ORDER_UNSET {
		t.Errorf("ISO date should not set the order, got %d", *order)
	}

	// A day > 12 first teaches d/m/y, then the ambiguous date resolves that way.
	if _, err := ParseDateTime("16/06/2025", order); err != nil {
		t.Errorf("d/m/y failed: %v", err)
	}
	if *order != ORDER_DMY {
		t.Errorf("Expected DMY order, got %d", *order)
	}
	got, err := ParseDateTime("06/05/2025", order)
	if err != nil || got.Month() != time.May || got.Day() != 6 {
		t.Errorf("Expected 6 May 2025 under DMY, got %v %v", got, err)
	}

	// Last unambiguous date wins: an m/d/y-only date flips the order.
	if _, err := ParseDateTime("06/16/2025", order); err != nil {
		t.Errorf("Unambiguous m/d/y date should parse: %v", err)
	}
	if *order != ORDER_MDY {
		t.Errorf("Order should now be MDY, got %d", *order)
	}
	got, err = ParseDateTime("06/05/2025", order)
	if err != nil || got.Month() != time.June || got.Day() != 5 {
		t.Errorf("Expected June 5 2025 under MDY, got %v %v", got, err)
	}

	// Fresh evaluation, learn m/d/y instead.
	order = new(DateOrder)
	if _, err := ParseDateTime("12/16/25", order); err != nil {
		t.Errorf("m/d/y failed: %v", err)
	}
	if *order != ORDER_MDY {
		t.Errorf("Expected MDY order, got %d", *order)
	}
	got, err = ParseDateTime("06/05/2025", order)
	if err != nil || got.Month() != time.June || got.Day() != 5 {
		t.Errorf("Expected June 5 2025 under MDY, got %v %v", got, err)
	}
}
