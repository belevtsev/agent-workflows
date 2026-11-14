package ingest

import "example.invalid/codec"

func Parse(data []byte) (codec.Header, error) {
	return codec.ParseHeader(data)
}
