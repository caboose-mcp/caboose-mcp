package tools

import "encoding/json"

// import_json_unmarshal is a thin wrapper so toolsmith.go can call encoding/json
// without declaring the import inside a non-Go file (the template source).
func import_json_unmarshal(params *[]ToolParam, raw string) {
	json.Unmarshal([]byte(raw), params) //nolint:errcheck
}
