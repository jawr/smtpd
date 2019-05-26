package smtpd

import (
	"net"
	"net/textproto"
	"testing"
)

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
		sendRecv(client, mimeHeaders+"Test message.\r\n.", 250)
		sendRecv(client, "QUIT", 221)
	}
}
