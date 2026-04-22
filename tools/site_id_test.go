package tools

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeSiteID_AcceptsAllFourFormats(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		// Short form, canonical.
		{"short Slussen", "9192", 9192},
		{"short T-Centralen", "9001", 9001},
		{"short Jakobsberg", "9702", 9702},
		{"short Stockholm Central 1002", "1002", 1002},
		{"short edge low 102", "102", 102},
		{"short edge high 9999", "9999", 9999},

		// 8-digit 18xx form (stop_finder properties.stopId).
		{"18xx Jakobsberg", "18009702", 9702},
		{"18xx Slussen", "18009192", 9192},
		{"18xx T-Centralen", "18009001", 9001},

		// 9-digit 3BA1CDEFG form (trafiklab docs).
		{"9-digit Slussen", "300109192", 9192},
		{"9-digit T-Centralen", "300109001", 9001},
		{"9-digit Jakobsberg", "300109702", 9702},

		// 16-digit GID form (stop_finder id; ported from trafiklab-sl Python).
		{"gid Slussen", "9091001000009192", 9192},
		{"gid T-Centralen", "9091001000009001", 9001},
		{"gid Jakobsberg", "9091001000009702", 9702},
		{"gid 5730 (python test case)", "9091001000005730", 5730},
		{"gid 9731 (python test case)", "9091001000009731", 9731},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeSiteID(tc.input)
			if err != nil {
				t.Fatalf("normalizeSiteID(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("normalizeSiteID(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeSiteID_InvalidFormat(t *testing.T) {
	// Non-numeric or empty inputs — we couldn't parse at all.
	cases := []string{
		"",
		"   ",
		"foo",
		"abc123",
		"9702.0",
		"9702x",
		"-9702",
		"0x9702",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := normalizeSiteID(in)
			var se *siteIDError
			if !errors.As(err, &se) {
				t.Fatalf("expected siteIDError, got %T: %v", err, err)
			}
			if se.Code != errInvalidSiteIDFormat {
				t.Errorf("expected %s, got %s", errInvalidSiteIDFormat, se.Code)
			}
		})
	}
}

func TestNormalizeSiteID_OutOfRange(t *testing.T) {
	// Numeric inputs that either parse to something outside 102..9999, or
	// have a length that doesn't match any recognized shape.
	cases := []string{
		"0",
		"42",                // 2 digits: no shape
		"101",               // short form below minSiteID
		"10000",             // 5 digits: no shape
		"99999999",          // 8 digits, wrong prefix (not "180")
		"99999",             // 5 digits: no shape
		"000000000",         // 9 digits, tail all zeros
		"9091001000000000",  // 16-digit GID with all-zero tail
		"1234567890123456",  // 16 digits, wrong prefix
		"180000000",         // 9 digits: extractShort takes last 5 = "00000" → 0 → out of range
		"99999999999999999", // 17 digits: no shape
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := normalizeSiteID(in)
			var se *siteIDError
			if !errors.As(err, &se) {
				t.Fatalf("expected siteIDError, got %T: %v", err, err)
			}
			if se.Code != errSiteIDOutOfRange {
				t.Errorf("expected %s, got %s", errSiteIDOutOfRange, se.Code)
			}
		})
	}
}

func TestNormalizeSiteID_ErrorJSONEnvelope(t *testing.T) {
	_, err := normalizeSiteID("foo")
	var se *siteIDError
	if !errors.As(err, &se) {
		t.Fatalf("expected siteIDError, got %v", err)
	}
	// asJSON must be a stable shape callers can match on.
	js := se.asJSON()
	if !strings.Contains(js, `"error":"invalid_site_id_format"`) {
		t.Errorf("missing error code in JSON: %s", js)
	}
	if !strings.Contains(js, `"input":"foo"`) {
		t.Errorf("missing input echo in JSON: %s", js)
	}
	if !strings.Contains(js, `"hint":`) {
		t.Errorf("missing hint in JSON: %s", js)
	}
}
