package smtpd

import (
	"io"
)

// MaxReader limits the size of the email DATA body being feed to the MIME parser
// Counting of bytes takes place after textproto.DotReader has decoded the body
type MaxReader struct {
	Reader    io.Reader
	BytesRead int
	MaxBytes  int
	// We could use a uint or int64, but we shouldn't have any emails over 4.2 GB
}

// Read bytes, count sheep
func (r *MaxReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.BytesRead += n
	if r.MaxBytes != 0 && r.BytesRead > r.MaxBytes {
		return n, maxSizeExceeded(r.MaxBytes)
	}
	return n, err
}
