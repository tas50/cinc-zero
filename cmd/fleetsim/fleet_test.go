package main

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func TestSelectStuckCountAndDeterminism(t *testing.T) {
	var names []string
	for i := range 100 {
		names = append(names, fmt.Sprintf("node%02d", i))
	}
	a := selectStuck(names, 0.02, rand.New(rand.NewSource(7)))
	if len(a) != 2 {
		t.Fatalf("got %d stuck, want 2 (ceil(0.02*100))", len(a))
	}
	b := selectStuck(names, 0.02, rand.New(rand.NewSource(7)))
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different sets: %v vs %v", a, b)
	}
	c := selectStuck(names, 0.02, rand.New(rand.NewSource(8)))
	if reflect.DeepEqual(a, c) {
		t.Fatalf("different seeds produced identical sets")
	}
}

func TestSelectStuckCeil(t *testing.T) {
	// 0.02 * 3 = 0.06 -> ceil = 1
	got := selectStuck([]string{"a", "b", "c"}, 0.02, rand.New(rand.NewSource(1)))
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
}
