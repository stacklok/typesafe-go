package main

import "testing"

func TestACDOC03SupportTriage(t *testing.T) {
	v, err := run()
	if err != nil || v != "billing" {
		t.Fatalf("%q %v", v, err)
	}
}
