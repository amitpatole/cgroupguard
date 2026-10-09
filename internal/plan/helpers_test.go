package plan_test

import (
	"bytes"
	"encoding/json"
)

func jsonUnmarshalStrict(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
