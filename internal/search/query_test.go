package search

import "testing"

func TestQueryMatch(t *testing.T) {
	doc := mustDoc(t, `{
		"name": "web01",
		"chef_environment": "production",
		"tags": ["frontend", "ssl"],
		"cpu": {"total": 8},
		"role": "WebServer"
	}`)
	fields := Flatten(doc)

	cases := []struct {
		query string
		want  bool
	}{
		{`*:*`, true},
		{`name:web01`, true},
		{`name:web02`, false},
		{`name:WEB01`, true},     // case-insensitive
		{`role:webserver`, true}, // value lowercased
		{`name:web*`, true},      // wildcard
		{`name:w?b01`, true},     // single-char wildcard
		{`name:nope*`, false},
		{`tags:ssl`, true},    // array membership
		{`total:8`, true},     // nested suffix key
		{`cpu_total:8`, true}, // full nested path
		{`chef_environment:production AND tags:ssl`, true},
		{`chef_environment:staging AND tags:ssl`, false},
		{`chef_environment:staging OR tags:ssl`, true},
		{`tags:ssl AND NOT name:web02`, true},
		{`tags:ssl AND NOT name:web01`, false},
		{`NOT name:web02`, true},
		{`-name:web02`, true},               // leading-dash negation
		{`name:web01 tags:frontend`, true},  // implicit AND
		{`name:web02 tags:frontend`, false}, // implicit AND, one fails
		{`(name:web01 OR name:web99) AND tags:ssl`, true},
		{`cpu_total:[1 TO 10]`, true}, // inclusive numeric range
		{`cpu_total:[10 TO 20]`, false},
		{`cpu_total:{8 TO 20}`, false}, // exclusive lower bound
		{`cpu_total:[* TO 10]`, true},  // open-ended range
		{`name:*`, true},               // field existence
		{`missing:*`, false},
		{`web01`, true}, // bare term, any field
		{`nonexistent`, false},
	}
	for _, c := range cases {
		q, err := Parse(c.query)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.query, err)
			continue
		}
		if got := q.Matches(fields); got != c.want {
			t.Errorf("Matches(%q) = %v, want %v", c.query, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, q := range []string{"", "(unclosed", "name:"} {
		if _, err := Parse(q); err == nil {
			t.Errorf("Parse(%q) expected error, got nil", q)
		}
	}
}
