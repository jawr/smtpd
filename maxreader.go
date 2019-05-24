package smtpd

import (
	"io"
)

// Wraps by textproto.DotReader when reading the DATA body

type MaxReader struct {
	Reader    io.Reader
	BytesRead int
	MaxBytes  int
}

func (r *MaxReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.BytesRead += n
	if r.BytesRead > r.MaxBytes {
		return n, maxSizeExceeded(r.MaxBytes)
	}
	return n, err
}
