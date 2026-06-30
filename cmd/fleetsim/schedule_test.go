package main

import (
	"math/rand"
	"testing"
	"time"
)

func TestPeriodWithinBounds(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	interval, splay := 30*time.Minute, 30*time.Minute
	for range 1000 {
		d := period(interval, splay, 1, rng)
		if d < interval || d >= interval+splay {
			t.Fatalf("period %v out of [%v, %v)", d, interval, interval+splay)
		}
	}
}

func TestPeriodNoSplayExact(t *testing.T) {
	d := period(10*time.Minute, 0, 1, rand.New(rand.NewSource(1)))
	if d != 10*time.Minute {
		t.Fatalf("period = %v, want 10m", d)
	}
}

func TestPeriodSpeedScales(t *testing.T) {
	d := period(60*time.Minute, 0, 60, rand.New(rand.NewSource(1)))
	if d != time.Minute {
		t.Fatalf("period = %v, want 1m", d)
	}
}
