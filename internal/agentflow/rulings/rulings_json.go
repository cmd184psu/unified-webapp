// Package rulings only reaches encoding/json for the area-map setting value.
package rulings

import "encoding/json"

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
