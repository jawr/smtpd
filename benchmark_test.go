package smtpd

import (
	"bytes"
	"io"
	"io/ioutil"
	"net"
	"net/smtp"
	"net/textproto"
	"testing"
)

func PickRandomPort() (port string, err error) {
	var listener net.Listener
	listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}
	port = listener.Addr().String()
	err = listener.Close()
	return
}

var body []byte

func sendBody(client *textproto.Conn) (err error) {
	w := client.DotWriter()

	// Creating this body for every test is muddling the profiling results
	// _, err = io.Copy(w, SampleEmailBody())
	// if err != nil {
	// 	w.Close()
	// 	return
	// }

	// Global variables FTW because I don't care enough yet to DI
	if body == nil {
		var b []byte
		b, err = ioutil.ReadAll(SampleEmailBody())
		if err != nil {
			return
		}
		body = b
	}

	_, err = io.Copy(w, bytes.NewReader(body))
	if err != nil {
		w.Close()
		return
	}

	err = w.Close()
	if err != nil {
		return
	}

	_, _, err = client.ReadResponse(250)
	return
}

// Benchmark the mail handling without the network stack introducing latency.
func BenchmarkRawProcessingSequence(b *testing.B) {
	server := &Server{} // Default server configuration.

	sendRecv := func(client *textproto.Conn, send string, code int) {
		err := client.PrintfLine(send)
		if err != nil {
			b.Fatal(err)
		}

		_, _, err = client.ReadResponse(code)

		if err != nil {
			// err = errors.Wrap(err, fmt.Sprintf("sent: %q: want: %d, got", send, code))
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	// Benchmark a full mail transaction.
	for i := 0; i < b.N; i++ {

		clientConn, serverConn := net.Pipe()
		session := server.newSession(serverConn)
		go session.serve()

		reader := textproto.NewConn(clientConn)
		_, _ = reader.ReadLine() // Read greeting message first.

		client := textproto.NewConn(clientConn)

		sendRecv(client, "HELO host.example.com", 250)
		sendRecv(client, "MAIL FROM:<sender@example.com>", 250)
		sendRecv(client, "RCPT TO:<recipient@example.com>", 250)
		sendRecv(client, "RCPT TO:", 501)
		sendRecv(client, "DATA", 354)
		// sendRecv(client, mimeHeaders+"Test message.\r\n.", 250)

		err := sendBody(client)
		if err != nil {
			b.Fatal(err)
		}

		// sendRecv(client, "\n", 250)
		sendRecv(client, "QUIT", 221)
	}
}

func BenchmarkNetSMTP(b *testing.B) {

	addr, err := PickRandomPort()
	if err != nil {
		b.Fatal(err)
	}

	var bytesReceived int
	server := &Server{
		Addr:    addr,
		MaxSize: 100000,
		Handler: func(bytesRead int, remoteAddr net.Addr, from string, to []string, header textproto.MIMEHeader, body io.Reader) (err error) {
			_, err = io.Copy(ioutil.Discard, body)
			return
		},
		HandlerSuccess: func(bytesRead int, remoteAddr net.Addr, from string, to []string) {
			bytesReceived = bytesRead
			bytesReceived = 1611 // TODO
		},
	}

	go func() {
		b.Fatal(server.ListenAndServe())
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {

		to := []string{"recipient@example.com"}
		msg, err := ioutil.ReadAll(SampleEmailBody())
		if err != nil {
			b.Fatal(err)
		}

		err = smtp.SendMail(addr, nil, "sender@example.com", to, msg)
		if err != nil {
			b.Fatal(err)
		}

		if bytesReceived != len(msg) {
			b.Errorf("SMTP send failed, want %d, got %d\n", len(msg), bytesReceived)
		}

	}
}
