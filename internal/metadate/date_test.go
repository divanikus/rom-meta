package metadate

import "testing"

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"1998-11-23T00:00:00-08:00": "1998-11-23",
		"2000-10-17T00:00:00+00:00": "2000-10-17",
		"Nov1, 2000":                "2000-11-01",
		"Jul 1, 1997":               "1997-07-01",
		"1 July 1997":               "1997-07-01",
		"November 2000":             "2000-11",
		"2000-10":                   "2000-10",
		"2000":                      "2000",
		"unknown":                   "unknown",
	}

	for input, expected := range tests {
		if actual := Normalize(input); actual != expected {
			t.Fatalf("Normalize(%q) = %q, want %q", input, actual, expected)
		}
	}
}
