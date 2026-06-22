package search

import "testing"

// FuzzParse exercises the hand-written Solr-query lexer/parser against
// arbitrary input. The contract under fuzzing: Parse must never panic or hang,
// it must return a non-nil query whenever it returns a nil error, and the
// resulting query must evaluate against a document without panicking. Rejecting
// malformed input with an error is acceptable — only crashes and hangs are
// bugs. The seed corpus doubles as regression coverage under `go test`.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"*:*",
		"name:web01",
		"name:web*",
		"name:web?1",
		"role:web AND chef_environment:prod",
		"role:web OR role:db",
		"NOT name:db",
		"-name:db",
		`tags:"a b c"`,
		"version:[1.0 TO 2.0]",
		"version:{1.0 TO 2.0}",
		"ipaddress:[* TO *]",
		"(a:1 OR b:2) AND c:3",
		"name:*",
		"a:1 b:2 c:3",
		"foo_bar_baz:1",
		"platform:ubuntu^2",
		"name:web~1",
		"((((a:1))))",
		":",
		"[",
		`"unterminated`,
		"a TO b",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	fields := map[string][]string{
		"name":             {"web01"},
		"role":             {"web", "db"},
		"chef_environment": {"prod"},
		"version":          {"1.5"},
		"tags":             {"a b c"},
		"ipaddress":        {"10.0.0.1"},
	}

	f.Fuzz(func(t *testing.T, q string) {
		query, err := Parse(q)
		if err != nil {
			return // a parse error is a valid outcome; it just must not crash
		}
		if query == nil {
			t.Fatalf("Parse(%q) returned a nil query with a nil error", q)
		}
		// Evaluation must not panic for any successfully-parsed query.
		_ = query.Matches(fields)
	})
}
