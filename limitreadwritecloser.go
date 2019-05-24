package smtpd

import (
	"errors"
	"io"
)

// This class wraps the net.Conn ReadWriteCloser to throw an error if the client
// sends an DATA body larger than allowed. This is wrapped by textproto,
// multipart, and possibily other structs higher up. That means we can't use
// maxSizeExceededError because it would just be burried.

type LimitReadWriteCloser struct {
	Reader    io.ReadCloser
	BytesRead int
	MaxBytes  int
}

func (r *LimitReadWriteCloser) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.BytesRead += n
	if r.BytesRead > r.MaxBytes {
		// return n, maxSizeExceeded(r.MaxBytes)
		return n, errors.New("LimitReadWriteCloser: Too much data!")
	}
	return n, err
}

// Write only exists to fulfill the interface
func (r *LimitReadWriteCloser) Write(p []byte) (n int, err error) {
	return 0, errors.New("LimitReadWriteCloser cannot be used in write context")
}

func (r *LimitReadWriteCloser) Close() error {
	return r.Reader.Close()
}
