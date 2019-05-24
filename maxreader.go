package smtpd

import (
	"io"
)

// This class wraps the net.Conn ReadWriteCloser to throw an error if the client
// sends an DATA body larger than allowed. This is wrapped by textproto,
// multipart, and possibily other structs higher up. That means we can't use
// maxSizeExceededError because it would just be burried.

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
