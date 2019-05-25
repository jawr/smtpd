package dot

import (
	"fmt"
	"net"
	"net/textproto"
	"testing"
	"time"
)

// Transparent handling of CRFL endings and leading/ending dots (.)

func TestDotEncoding(t *testing.T) {
	var err error
	clientConn, serverConn := net.Pipe()

	serverConn.SetReadDeadline(time.Now().Add(time.Second))
	serverConn.SetWriteDeadline(time.Now().Add(time.Second))
	clientConn.SetReadDeadline(time.Now().Add(time.Second))
	clientConn.SetWriteDeadline(time.Now().Add(time.Second))

	s := textproto.NewConn(serverConn)
	c := textproto.NewConn(clientConn)

	go func() {
		err = s.PrintfLine("220 %s ESMTP Service ready", "localhost")
		if err != nil {
			t.Error(err)
		}

		err = s.W.Flush()
		if err != nil {
			t.Error(err)
		}
	}()

	var msg string
	_, msg, err = c.ReadCodeLine(220)
	if err != nil {
		t.Error(err)
	}

	fmt.Println(msg)

}
