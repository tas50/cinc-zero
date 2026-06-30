package main

import (
	"math/rand"
	"time"
)

// period returns the wall-clock duration a converging node sleeps before its
// next check-in: a base interval plus up to splay of random jitter, compressed
// by speed. With splay==0 it is exactly interval/speed. speed<=0 is treated as 1.
func period(interval, splay time.Duration, speed float64, rng *rand.Rand) time.Duration {
	sim := interval
	if splay > 0 {
		sim += time.Duration(rng.Int63n(int64(splay)))
	}
	if speed <= 0 {
		speed = 1
	}
	return time.Duration(float64(sim) / speed)
}
