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

func TestFractionalSeconds(t *testing.T) {
	// Every length of fraction parses and is kept.
	// 1, 2 and 4 digits matter most: those lengths are also the day, month and year tokens.
	fractions := []struct {
		digits     string
		nanosecond int
	}{
		{"1", 100000000},
		{"14", 140000000},
		{"146", 146000000},
		{"1463", 146300000},
		{"14630", 146300000},
		{"146300", 146300000},
		{"1463007", 146300700},
		{"14630078", 146300780},
		{"146300789", 146300789},
		{"1463007891", 146300789}, // Digits past the ninth are dropped.
		{"05", 50000000},
		{"0005", 500000},
	}
	for _, fraction := range fractions {
		for _, suffix := range []string{"", "Z"} {
			dateStr := "2026-08-17T15:00:32." + fraction.digits + suffix
			HelpTestDateTime(dateStr, 2026, time.August, 17, 15, 0, 32, t)
			parsedTime, _ := ParseDateTime(dateStr, new(DateOrder))
			if parsedTime.Nanosecond() != fraction.nanosecond {
				t.Errorf("%s: Parsed nanosecond %d, expected %d", dateStr, parsedTime.Nanosecond(), fraction.nanosecond)
			}
		}
	}

	helpNanosecond := func(dateStr string, expected int) {
		parsedTime, err := ParseDateTime(dateStr, new(DateOrder))
		if err != nil {
			t.Errorf("%s: %v", dateStr, err)
		} else if parsedTime.Nanosecond() != expected {
			t.Errorf("%s: Parsed nanosecond %d, expected %d", dateStr, parsedTime.Nanosecond(), expected)
		}
	}

	// The separator before the fraction does not matter, and AM/PM may follow it.
	HelpTestDateTime("2026-08-17 15:00:32,1463", 2026, time.August, 17, 15, 0, 32, t)
	helpNanosecond("2026-08-17 15:00:32,1463", 146300000)
	HelpTestDateTime("8/17/2026 3:00:32.1463 PM", 2026, time.August, 17, 15, 0, 32, t)
	helpNanosecond("8/17/2026 3:00:32.1463 PM", 146300000)
	HelpTestDateTime("8/17/2026 12:00:32.14 AM", 2026, time.August, 17, 0, 0, 32, t)
	helpNanosecond("8/17/2026 12:00:32.14 AM", 140000000)

	// No fraction is zero nanoseconds.
	helpNanosecond("2026-08-17T15:00:32Z", 0)
	helpNanosecond("8/17/2026 3:00:32 PM", 0)

	// Anything after the fraction is ignored.
	HelpTestDateTime("2026-08-17T15:00:32.1463 99", 2026, time.August, 17, 15, 0, 32, t)
	helpNanosecond("2026-08-17T15:00:32.1463 99", 146300000)
	HelpTestDateTime("2026-08-17T15:00:32.1463 PM 99 Aug 2027", 2026, time.August, 17, 15, 0, 32, t)
	HelpTestDateTime("8/17/2026 3:00:32 PM 99", 2026, time.August, 17, 15, 0, 32, t)

	// A number of another length where a date or time component belongs is an error.
	for _, bad := range []string{"2026-08-17 123:00", "2026-08-17 15:123", "2026-08-17 15:00:123", "2026-123-17"} {
		if _, err := ParseDateTime(bad, new(DateOrder)); err == nil {
			t.Errorf("Expected '%s' to fail to parse", bad)
		}
	}
}

func TestUtcOffsetIgnored(t *testing.T) {
	// Each input is 15:00:32 on 17 August 2026 with the given nanoseconds. The offset is
	// never applied and is never read as the fraction of a second.
	cases := []struct {
		in         string
		nanosecond int
	}{
		{"2026-08-17T15:00:32+05:30", 0},
		{"2026-08-17T15:00:32-05:00", 0},
		{"2026-08-17T15:00:32+0530", 0},
		{"2026-08-17T15:00:32-0500", 0},
		{"2026-08-17T15:00:32+05", 0},
		{"2026-08-17T15:00:32-05", 0},
		{"2026-08-17 15:00:32 +05:30", 0},
		{"2026-08-17 15:00:32 - 05:00", 0},
		{"2026-08-17 15:00:32 UTC+5", 0},
		{"2026-08-17 15:00:32 GMT+5:30", 0},
		{"2026-08-17T15:00:32.1463+05:30", 146300000},
		{"2026-08-17T15:00:32,1463-05:00", 146300000},
		{"2026-08-17 15:00:32.1463+00", 146300000},          // PostgreSQL
		{"2026-08-17 15:00:32.1463 +0000 UTC", 146300000},   // Go time.String()
		{"Mon, 17 Aug 2026 15:00:32 +0000", 0},              // Email
		{"Mon Aug 17 2026 15:00:32 GMT+0530 (India Standard Time)", 0}, // JavaScript Date.toString()
		{"8/17/2026 3:00:32 PM -05:00", 0},
		{"8/17/2026 3:00:32.5 PM +05:30", 500000000},
		{"2026-08-17T15:00:32\u221205:00", 0}, // minus sign
		{"2026-08-17T15:00:32\u201305:00", 0}, // en dash
		{"2026-08-17T15:00:32\uFF0D05:00", 0}, // fullwidth hyphen-minus
		{"2026-08-17T15:00:32\uFF0B05:30", 0}, // fullwidth plus sign
		{"2026-08-17T15:00:32 \u2212 05:00", 0},
		{"8/17/2026 3:00:32\u202FPM", 0}, // narrow no-break space before PM
		// The file name style uses '-' between the time parts, so '-' is not an offset there.
		{"2026-08-17T15-00-32", 0},
		{"2026-08-17_15-00-32-146", 146000000},
		{"2026-08-17_15-00-32+05", 0},
		// With no sign, the number after the seconds is the fraction.
		{"2026-08-17 15:00:32 05", 50000000},
	}
	for _, c := range cases {
		HelpTestDateTime(c.in, 2026, time.August, 17, 15, 0, 32, t)
		parsedTime, _ := ParseDateTime(c.in, new(DateOrder))
		if parsedTime.Nanosecond() != c.nanosecond {
			t.Errorf("%s: Parsed nanosecond %d, expected %d", c.in, parsedTime.Nanosecond(), c.nanosecond)
		}
	}

	// An offset after a time with no seconds, after only an hour, and after only a date.
	HelpTestDateTime("2026-08-17T15:00+05:30", 2026, time.August, 17, 15, 0, 0, t)
	HelpTestDateTime("2026-08-17T15:00-05:00", 2026, time.August, 17, 15, 0, 0, t)
	HelpTestDateTime("8/17/2026 3:00 PM -05:00", 2026, time.August, 17, 15, 0, 0, t)
	HelpTestDateTime("8/17/2026 3:00 PM +0530", 2026, time.August, 17, 15, 0, 0, t)
	HelpTestDateTime("2026-08-17T15+05:30", 2026, time.August, 17, 15, 0, 0, t)
	HelpTestDateTime("2026-08-17+05:30", 2026, time.August, 17, 0, 0, 0, t)
	// A '-' after the hour or the date is a separator, as in the file name style.
	HelpTestDateTime("2026-08-17T15-05", 2026, time.August, 17, 15, 5, 0, t)
	HelpTestDateTime("2026-08-17-15-05-09", 2026, time.August, 17, 15, 5, 9, t)
}

func TestDateTokenPrev(t *testing.T) {
	// Number tokens of "2026-08-17T15:00:32.1463 \u2212 05:00  PM", with the punctuation before each.
	l := NewDateLexer("2026-08-17T15:00:32.1463 \u2212 05:00  PM")
	expected := []struct {
		lexeme string
		prev   byte
	}{
		{"2026", 0}, {"08", '-'}, {"17", '-'}, {"15", 0}, {"00", ':'}, {"32", ':'},
		{"1463", '.'}, {"05", '-'}, {"00", ':'}, {"PM", 0},
	}
	i := 0
	for {
		token := l.scanToken()
		if token.Type == DATEEOF {
			break
		}
		if token.Type == DATESEP {
			if token.Lexeme == " " {
				t.Errorf("Whitespace should not be a token")
			}
			continue
		}
		if i >= len(expected) {
			t.Fatalf("Unexpected extra token '%s'", token.Lexeme)
		}
		if token.Lexeme != expected[i].lexeme || token.Prev != expected[i].prev {
			t.Errorf("Token %d: got '%s' with previous punctuation %q, expected '%s' with %q", i, token.Lexeme, token.Prev, expected[i].lexeme, expected[i].prev)
		}
		i++
	}
	if i != len(expected) {
		t.Errorf("Got %d tokens, expected %d", i, len(expected))
	}
}

func TestHourWithMeridiem(t *testing.T) {
	// An hour with AM/PM and no minutes.
	HelpTestDateTime("2026-08-17 5 PM", 2026, time.August, 17, 17, 0, 0, t)
	HelpTestDateTime("2026-08-17 5PM", 2026, time.August, 17, 17, 0, 0, t)
	HelpTestDateTime("2026-08-17 5 p.m.", 2026, time.August, 17, 17, 0, 0, t)
	HelpTestDateTime("2026-08-17 5 AM", 2026, time.August, 17, 5, 0, 0, t)
	HelpTestDateTime("2026-08-17 12 AM", 2026, time.August, 17, 0, 0, 0, t)
	HelpTestDateTime("2026-08-17 12 PM", 2026, time.August, 17, 12, 0, 0, t)
	HelpTestDateTime("Aug 17, 2026 11 PM", 2026, time.August, 17, 23, 0, 0, t)
	// A UTC offset after it is ignored.
	HelpTestDateTime("2026-08-17 5 PM -05:00", 2026, time.August, 17, 17, 0, 0, t)
	HelpTestDateTime("2026-08-17 5 PM +0530", 2026, time.August, 17, 17, 0, 0, t)

	// A number with no sign after the AM/PM is still an error, as it is after a minute.
	for _, bad := range []string{"2026-08-17 5 PM 30", "2026-08-17 5:30 PM 30"} {
		if _, err := ParseDateTime(bad, new(DateOrder)); err == nil {
			t.Errorf("Expected '%s' to fail to parse", bad)
		}
	}
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
