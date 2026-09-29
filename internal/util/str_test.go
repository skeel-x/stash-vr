package util

import "testing"

func TestStrSliceEquals(t *testing.T) {
	tests := []struct {
		name string
		s    string
		ss   []string
		v    string
		want bool
	}{
		{name: "exact name", s: "Blonde", ss: nil, v: "Blonde", want: true},
		{name: "name differs in case", s: "Blonde", ss: nil, v: "bLONDE", want: true},
		{name: "alias exact", s: "Blonde", ss: []string{"Fair", "Golden"}, v: "Golden", want: true},
		{name: "alias differs in case", s: "Blonde", ss: []string{"Fair", "Golden"}, v: "fair", want: true},
		{name: "no match", s: "Blonde", ss: []string{"Fair", "Golden"}, v: "Brunette", want: false},
		{name: "prefix is not a match", s: "Blonde", ss: []string{"Fair"}, v: "Blond", want: false},
		{name: "empty inputs", s: "", ss: nil, v: "", want: true},
		{name: "empty value against name", s: "Blonde", ss: nil, v: "", want: false},
		{name: "unicode fold", s: "straße", ss: nil, v: "STRASSE", want: false},
		{name: "unicode simple fold", s: "Éclair", ss: nil, v: "éCLAIR", want: true},
		{name: "kelvin sign folds to k", s: "Kelvin", ss: nil, v: "kelvin", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StrSliceEquals(tt.s, tt.ss, tt.v); got != tt.want {
				t.Fatalf("StrSliceEquals(%q, %q, %q) = %v, want %v", tt.s, tt.ss, tt.v, got, tt.want)
			}
		})
	}
}

func BenchmarkStrSliceEquals(b *testing.B) {
	aliases := []string{"Fair", "Golden", "Platinum", "Sandy"}
	for b.Loop() {
		StrSliceEquals("Blonde", aliases, "sandy")
	}
}
