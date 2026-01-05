package integration

import "encoding/json"

func ToUnsafeJSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(b)
}
