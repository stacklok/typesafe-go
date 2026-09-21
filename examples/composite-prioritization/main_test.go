package main

import "testing"

func TestACDOC03CallerOwnedWeighting(t *testing.T) {
	a, e := priority(1)
	b, e2 := priority(0)
	if e != nil || e2 != nil || a != 1.5 || b != .8 {
		t.Fatalf("%v %v %v %v", a, b, e, e2)
	}
}
