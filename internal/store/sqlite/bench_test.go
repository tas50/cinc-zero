package sqlite_test

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tas50/cinc-zero/internal/store/sqlite"
)

// nodeBody is a representative chef-client node document, the shape the fleet
// check-in path round-trips on every request.
func nodeBody(name string) []byte {
	return []byte(fmt.Sprintf(`{"name":%q,"chef_environment":"production","json_class":"Chef::Node",`+
		`"chef_type":"node","normal":{"tags":["a","b","c"]},`+
		`"automatic":{"os":"linux","ohai_time":1700000000.0,"memory":{"total":"16gb"},`+
		`"ipaddress":"10.0.0.1","network":{"interfaces":{"eth0":{"addr":"10.0.0.1"}}}},`+
		`"default":{},"override":{},"run_list":["recipe[nginx]","recipe[base]"]}`, name))
}

func benchBackend(b *testing.B) *sqlite.Backend {
	be, err := sqlite.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { be.Close() })
	return be
}

// BenchmarkGet measures single-row read latency through the backend (the GET
// half of a check-in). It is dominated by the driver's per-call statement
// handling, not I/O, since the row is already cached.
func BenchmarkGet(b *testing.B) {
	be := benchBackend(b)
	if err := be.Put("acme", "nodes", "node0", nodeBody("node0")); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok, err := be.Get("acme", "nodes", "node0"); err != nil || !ok {
			b.Fatalf("get: ok=%v err=%v", ok, err)
		}
	}
}

// BenchmarkPut measures single-row write (upsert) latency through the backend
// (the PUT half of a check-in).
func BenchmarkPut(b *testing.B) {
	be := benchBackend(b)
	body := nodeBody("node0")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := be.Put("acme", "nodes", "node0", body); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCheckin models the steady-state fleet workload: a GET of a node
// followed by a PUT of the same node, run concurrently across the fleet.
func BenchmarkCheckin(b *testing.B) {
	const nodes = 1107
	be := benchBackend(b)
	for i := 0; i < nodes; i++ {
		n := fmt.Sprintf("node%d", i)
		if err := be.Put("acme", "nodes", n, nodeBody(n)); err != nil {
			b.Fatal(err)
		}
	}
	var seq atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			n := fmt.Sprintf("node%d", int(seq.Add(1))%nodes)
			if _, _, err := be.Get("acme", "nodes", n); err != nil {
				b.Fatal(err)
			}
			if err := be.Put("acme", "nodes", n, nodeBody(n)); err != nil {
				b.Fatal(err)
			}
		}
	})
}
