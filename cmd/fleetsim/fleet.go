package main

import (
	"math"
	"math/rand"
	"sort"
)

// node is one fleet member: its name and last-known full JSON body.
type node struct {
	name string
	body []byte
}

// selectStuck deterministically chooses ceil(frac*len(names)) node names that
// never check in. Sorting first makes the result independent of input order;
// the caller-seeded rng makes it reproducible.
func selectStuck(names []string, frac float64, rng *rand.Rand) map[string]bool {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	rng.Shuffle(len(sorted), func(i, j int) { sorted[i], sorted[j] = sorted[j], sorted[i] })
	k := min(int(math.Ceil(frac*float64(len(sorted)))), len(sorted))
	stuck := make(map[string]bool, k)
	for _, name := range sorted[:k] {
		stuck[name] = true
	}
	return stuck
}
