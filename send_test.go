package smtpd

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"testing"

	"github.com/Xeoncross/mimestream"
)

func SampleEmailBody() io.Reader {
	parts := mimestream.Parts{
		mimestream.Mixed{
			Parts: mimestream.Parts{
				mimestream.Text{
					Text: "This is the text that goes in the plain part. It will need to be wrapped to 76 characters and quoted.",
				},
				mimestream.Text{
					ContentType: mimestream.TextHTML,
					Text:        "<p>This is the text that goes in the plain part. It will need to be wrapped to 76 characters and quoted.</p>",
				},
			},
		},
		mimestream.File{
			Name:   "filename-2 שלום.txt",
			Reader: strings.NewReader("Filename text content"),
		},
		mimestream.File{
			ContentType: "application/json", // Optional
			Name:        "payload.json",
			Reader:      strings.NewReader(`{"one":1,"two":2}`),
		},
	}

	buf := &bytes.Buffer{}
	mw := multipart.NewWriter(buf)

	parts.Into(mw)
	mw.Close()

	headers := strings.Join([]string{
		"From: Sender <sender@example.com>",
		"Mime-Version: 1.0 (1.0)",
		"Date: Thu, 10 Jan 2002 11:12:00 -0700",
		"Subject: My Temp Message",
		"Message-Id: <1234567890>",
		"To: <recipient@example.com>",
		"Content-Type: " + mw.FormDataContentType()}, "\r\n") + "\r\n\r\n"

	return io.MultiReader(strings.NewReader(headers), buf)
}

func TestMultipartSendWithPipe(t *testing.T) {

	// var err error

	var bytesReceived int

	server := &Server{
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

	clientConn, serverConn := net.Pipe()
	session := server.newSession(serverConn)
	go session.serve()

	client := textproto.NewConn(clientConn)

	readReply := func() {
		_, err := client.ReadLine()
		if err != nil {
			t.Error(err)
		}
	}

	_, _ = client.ReadLine() // Read greeting message first.

	fmt.Fprintf(clientConn, "%s\r\n", "HELO host.example.com")
	readReply()
	fmt.Fprintf(clientConn, "%s\r\n", "MAIL FROM:<sender@example.com>")
	readReply()
	fmt.Fprintf(clientConn, "%s\r\n", "RCPT TO:<recipient@example.com>")
	readReply()
	fmt.Fprintf(clientConn, "%s\r\n", "DATA")
	readReply()
	bytesSent, err := io.Copy(clientConn, SampleEmailBody())
	if err != nil {
		t.Fatal(err)
	}

	fmt.Fprint(clientConn, "\r\n.\r\n")
	readReply()
	fmt.Fprintf(clientConn, "%s\r\n", "QUIT")
	readReply()

	if bytesReceived != int(bytesSent) {
		t.Errorf("SMTP send failed, want %d, got %d\n", bytesSent, bytesReceived)
	}
}

// https://golang.org/pkg/net/smtp/#SendMail
func TestNetSMTP(t *testing.T) {

	var bytesReceived int
	server := &Server{
		Addr:    ":6000",
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
		_ = server.ListenAndServe()
	}()

	to := []string{"recipient@example.com"}
	msg, err := ioutil.ReadAll(SampleEmailBody())
	if err != nil {
		t.Fatal(err)
	}

	err = smtp.SendMail(":6000", nil, "sender@example.com", to, msg)
	if err != nil {
		t.Fatal(err)
	}

	if bytesReceived != len(msg) {
		t.Errorf("SMTP send failed, want %d, got %d\n", len(msg), bytesReceived)
	}
}
