package smtpd

import (
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net"
	"net/textproto"
	"strings"
	"testing"

	"github.com/Xeoncross/mimestream"
)

func TestMultipartSend(t *testing.T) {

	var err error

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

	server := &Server{
		MaxSize: 100000,
		Handler: func(remoteAddr net.Addr, from string, to []string, header textproto.MIMEHeader, body io.Reader) error {
			// fmt.Printf("Received mail from %s to %s\n", from, to)

			// Ignore body
			_, err = io.Copy(ioutil.Discard, body)
			if err != nil {
				return err
			}

			return nil
		},
	} // Default server configuration.
	clientConn, serverConn := net.Pipe()
	session := server.newSession(serverConn)
	go session.serve()

	// To pipe to reader
	pr, pw := io.Pipe()

	mw := multipart.NewWriter(pw)

	headers := strings.Join([]string{
		"From: Sender <sender@example.com>",
		"Mime-Version: 1.0 (1.0)",
		"Date: Thu, 10 Jan 2002 11:12:00 -0700",
		"Subject: My Temp Message",
		"Message-Id: <1234567890>",
		"To: <recipient@example.com>",
		"Content-Type: " + mw.FormDataContentType()}, "\r\n") + "\r\n\r\n"

	// writing without a reader will deadlock so write in a goroutine
	go func() {
		// Start the pipeline
		err = parts.Into(mw)
		if err != nil {
			t.Error(err)
		}
		pw.Close()
	}()

	mailreader := io.MultiReader(strings.NewReader(headers), pr)

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

	io.Copy(clientConn, mailreader)
	fmt.Fprint(clientConn, "\r\n.\r\n")
	readReply()

	fmt.Fprintf(clientConn, "%s\r\n", "QUIT")
	readReply()
}
