// Package dates holds the calendar arithmetic the app depends on.
//
// Everything is keyed by a calendar date in the user's own timezone, written
// as YYYY-MM-DD. "Today" is computed in that zone, never the server's, or an
// 11pm entry lands on the wrong day.
package dates

import (
	"fmt"
	"time"
)

// Layout is the wire format for a calendar date.
const Layout = "2006-01-02"

// LoadLocation resolves an IANA name, falling back to UTC for anything
// unknown rather than failing the request.
func LoadLocation(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Today is the calendar date now, in loc.
func Today(loc *time.Location) string {
	return time.Now().In(loc).Format(Layout)
}

// Local formats t as a calendar date in loc.
func Local(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(Layout)
}

// Valid reports whether s is a well-formed YYYY-MM-DD date.
func Valid(s string) bool {
	_, err := time.Parse(Layout, s)
	return err == nil
}

// Parse reads a YYYY-MM-DD date at midnight UTC.
func Parse(s string) (time.Time, error) {
	t, err := time.Parse(Layout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("dates: %q is not a YYYY-MM-DD date", s)
	}
	return t, nil
}

// AddDays shifts a date by n days.
func AddDays(date string, n int) string {
	t, err := Parse(date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format(Layout)
}

// Range lists every date from from to to inclusive. An inverted range is
// empty rather than an error.
func Range(from, to string) []string {
	start, err := Parse(from)
	if err != nil {
		return nil
	}
	end, err := Parse(to)
	if err != nil {
		return nil
	}
	var out []string
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(Layout))
	}
	return out
}

// DaysBetween is to minus from in whole days.
func DaysBetween(from, to string) int {
	a, err := Parse(from)
	if err != nil {
		return 0
	}
	b, err := Parse(to)
	if err != nil {
		return 0
	}
	return int(b.Sub(a).Hours() / 24)
}
