package bundled

import (
	"context"
	"errors"
)

// Provider is lazy: help, version and unrelated read-only commands need not
// decode a payload. Tests may supply a tiny, independently constructed bundle.
type Provider func(context.Context) (*Bundle, error)

// The release builder initializes embeddedRaw in generated Go source inside a
// disposable exact export. Ordinary source builds intentionally have no payload.
var embeddedRaw []byte
var embeddedMarker string

// Embedded validates the carried resource pack only when it is requested.
func Embedded(ctx context.Context) (*Bundle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(embeddedRaw) == 0 || embeddedMarker == "" {
		return nil, errors.New("this developer binary has no embedded resources; use a standalone release or explicit developer source mode")
	}
	bundle, err := Open(embeddedRaw)
	if err != nil {
		return nil, err
	}
	marker, err := BinaryMarker(bundle.Identity())
	if err != nil || marker != embeddedMarker {
		return nil, errors.New("embedded resources differ from the executable bundle identity")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bundle, nil
}
