package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// marshalNoHTML returns json's encoding of s as a string with HTML escaping
// disabled — exactly what writeJSON produces for string values. It is the oracle
// appendJSONStringContent must match (with quotes added).
func marshalNoHTML(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatal(err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

func TestAppendJSONStringContentMatchesEncodingJSON(t *testing.T) {
	cases := []string{
		"",
		"node0",
		"simple-name_1:2.3",
		"with\"quote",
		"with\\backslash",
		"tab\tnewline\ncr\r",
		"bell\x07null\x00unit\x1f",
		"html <b> & 'amp' > end", // not escaped when HTML escaping is off
		"unicode: café résumé",
		"emoji \U0001F680 and 汉字",
		"a b c", // line/para separators: encoding/json escapes these
		"trailing space ",
		"\x00",
		string([]byte{0xff, 0xfe, 'a'}), // invalid UTF-8 → U+FFFD
	}
	for _, s := range cases {
		got := `"` + string(appendJSONStringContent(nil, s)) + `"`
		want := marshalNoHTML(t, s)
		if got != want {
			t.Errorf("appendJSONStringContent(%q):\n got = %s\nwant = %s", s, got, want)
		}
	}
}
