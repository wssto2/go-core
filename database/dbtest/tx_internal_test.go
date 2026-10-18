package dbtest

import "testing"

func TestWithClientFoundRows(t *testing.T) {
	for in, want := range map[string]string{
		"u:p@tcp(h:3306)/db":                          "u:p@tcp(h:3306)/db?clientFoundRows=true",
		"u:p@tcp(h:3306)/db?parseTime=true":           "u:p@tcp(h:3306)/db?parseTime=true&clientFoundRows=true",
		"u:p@tcp(h:3306)/db?clientFoundRows=false":    "u:p@tcp(h:3306)/db?clientFoundRows=false",
		"u:p@tcp(h:3306)/db?a=b&clientFoundRows=true": "u:p@tcp(h:3306)/db?a=b&clientFoundRows=true",
	} {
		if got := withClientFoundRows(in); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}
