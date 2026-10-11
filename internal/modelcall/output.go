package modelcall

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/soaringjerry/PCAS/internal/memory"
)

// DecodeStrict reads a model's structured output. It rejects an unknown field
// and any content after the first value, so that a caller never applies part
// of an output that it did not understand.
func DecodeStrict(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return memory.ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return memory.ErrInvalid
	}
	return nil
}
