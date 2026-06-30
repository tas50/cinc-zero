package main

import "encoding/json"

// stampOhaiTime returns body with automatic.ohai_time set to ts (Unix seconds),
// creating the automatic map if absent. Every other field is preserved.
func stampOhaiTime(body []byte, ts int64) ([]byte, error) {
	var node map[string]any
	if err := json.Unmarshal(body, &node); err != nil {
		return nil, err
	}
	automatic, ok := node["automatic"].(map[string]any)
	if !ok {
		automatic = map[string]any{}
		node["automatic"] = automatic
	}
	automatic["ohai_time"] = float64(ts)
	return json.Marshal(node)
}
