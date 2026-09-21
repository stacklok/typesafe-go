package main

import "testing"

func TestACDOC03CandidateExtraction(t *testing.T) {
	v, err := selectCandidate("AB-12 CD-34")
	if err != nil || v != "AB-12" {
		t.Fatalf("%q %v", v, err)
	}
}
