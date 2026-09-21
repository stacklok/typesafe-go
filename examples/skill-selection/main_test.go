package main

import "testing"

func TestACDOC03SkillSelectionAbstainsOrRecommendsOnly(t *testing.T) {
	for _, test := range []struct {
		confidence float64
		want       string
	}{{.5, "review"}, {.8, "recommend-go"}} {
		v, err := recommend(test.confidence)
		if err != nil || v != test.want {
			t.Fatalf("%q %v; want %q", v, err, test.want)
		}
	}
}
