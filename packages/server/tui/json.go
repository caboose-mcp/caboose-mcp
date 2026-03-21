package tui

import "encoding/json"

func jsonUnmarshalSimple(data []byte, v any) error { return json.Unmarshal(data, v) }
func jsonMarshalSimple(v any) ([]byte, error)      { return json.Marshal(v) }
