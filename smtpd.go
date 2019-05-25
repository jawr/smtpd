// Package smtpd implements a basic SMTP server.
package smtpd

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net"
	"net/textproto"
	"os"
	"regexp"
	"time"
)

var (
	// Debug `true` enables verbose logging.
	Debug      = true
	rcptToRE   = regexp.MustCompile(`[Tt][Oo]:<(.+)>`)
	mailFromRE = regexp.MustCompile(`[Ff][Rr][Oo][Mm]:<(.*)>(\s(.*))?`) // Delivery Status Notifications are sent with "MAIL FROM:<>"
	mailSizeRE = regexp.MustCompile(`[Ss][Ii][Zz][Ee]=(\d+)`)
)

// Handler function called upon successful receipt of an email.
type Handler func(remoteAddr net.Addr, from string, to []string, data []byte)

// HandlerRcpt function called on RCPT. Return accept status.
type HandlerRcpt func(remoteAddr net.Addr, from string, to string) bool

// ListenAndServe listens on the TCP network address addr
// and then calls Serve with handler to handle requests
// on incoming connections.
func ListenAndServe(addr string, handler Handler, appname string, hostname string) error {
	srv := &Server{Addr: addr, Handler: handler, Appname: appname, Hostname: hostname}
	return srv.ListenAndServe()
}

// ListenAndServeTLS listens on the TCP network address addr
// and then calls Serve with handler to handle requests
// on incoming connections. Connections may be upgraded to TLS if the client requests it.
func ListenAndServeTLS(addr string, certFile string, keyFile string, handler Handler, appname string, hostname string) error {
	srv := &Server{Addr: addr, Handler: handler, Appname: appname, Hostname: hostname}
	err := srv.ConfigureTLS(certFile, keyFile)
	if err != nil {
		return err
	}
	return srv.ListenAndServe()
}

type maxSizeExceededError struct {
	limit int
}

func maxSizeExceeded(limit int) maxSizeExceededError {
	return maxSizeExceededError{limit}
}

// Error uses the RFC 5321 response message in preference to RFC 1870.
// RFC 3463 defines enhanced status code x.3.4 as "Message too big for system".
func (err maxSizeExceededError) Error() string {
	return fmt.Sprintf("552 5.3.4 Requested mail action aborted: exceeded storage allocation (%d)", err.limit)
}

// LogFunc is a function capable of logging the client-server communication.
type LogFunc func(remoteIP, verb, line string)

// Server is an SMTP server.
type Server struct {
	Addr        string // TCP address to listen on, defaults to ":25" (all addresses, port 25) if empty
	Appname     string
	Handler     Handler
	HandlerRcpt HandlerRcpt
	Hostname    string
	LogRead     LogFunc
	LogWrite    LogFunc
	MaxSize     int // Maximum message size allowed, in bytes
	Timeout     time.Duration
	TLSConfig   *tls.Config
	TLSListener bool // Listen for incoming TLS connections only (not recommended as it may reduce compatibility). Ignored if TLS is not configured.
	TLSRequired bool // Require TLS for every command except NOOP, EHLO, STARTTLS, or QUIT as per RFC 3207. Ignored if TLS is not configured.
}

// ConfigureTLS creates a TLS configuration from certificate and key files.
func (srv *Server) ConfigureTLS(certFile string, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	return nil
}

// ConfigureTLSWithPassphrase creates a TLS configuration from a certificate,
// an encrypted key file and the associated passphrase:
func (srv *Server) ConfigureTLSWithPassphrase(
	certFile string,
	keyFile string,
	passphrase string,
) error {
	certPEMBlock, err := ioutil.ReadFile(certFile)
	if err != nil {
		return err
	}
	keyPEMBlock, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return err
	}
	keyDERBlock, _ := pem.Decode(keyPEMBlock)
	keyPEMDecrypted, err := x509.DecryptPEMBlock(keyDERBlock, []byte(passphrase))
	if err != nil {
		return err
	}
	var pemBlock pem.Block
	pemBlock.Type = keyDERBlock.Type
	pemBlock.Bytes = keyPEMDecrypted
	keyPEMBlock = pem.EncodeToMemory(&pemBlock)
	cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
	if err != nil {
		return err
	}
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
	return nil
}

// ListenAndServe listens on the TCP network address srv.Addr and then
// calls Serve to handle requests on incoming connections.  If
// srv.Addr is blank, ":25" is used.
func (srv *Server) ListenAndServe() error {
	if srv.Addr == "" {
		srv.Addr = ":25"
	}
	if srv.Appname == "" {
		srv.Appname = "smtpd"
	}
	if srv.Hostname == "" {
		srv.Hostname, _ = os.Hostname()
	}
	if srv.Timeout == 0 {
		srv.Timeout = 5 * time.Minute
	}

	var ln net.Listener
	var err error

	// If TLSListener is enabled, listen for TLS connections only.
	if srv.TLSConfig != nil && srv.TLSListener {
		ln, err = tls.Listen("tcp", srv.Addr, srv.TLSConfig)
	} else {
		ln, err = net.Listen("tcp", srv.Addr)
	}
	if err != nil {
		return err
	}
	return srv.Serve(ln)
}

// Serve creates a new SMTP session after a network connection is established.
func (srv *Server) Serve(ln net.Listener) error {
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				continue
			}
			return err
		}
		// TODO request throttler
		session := srv.newSession(conn)
		go session.serve()
	}
}

// Create new session from connection.
func (srv *Server) newSession(conn net.Conn) (s *session) {

	s = &session{
		srv:  srv,
		conn: conn,
		// br:   bufio.NewReader(conn),
		// bw:   bufio.NewWriter(conn),

		// textproto is our gateway to DotReader/DotWriter for SMTP lines
		// It can add/remove \r\n and the leading/ending DATA dot markers (.)
		// https://golang.org/src/net/textproto/reader.go#L281
		// https://golang.org/src/net/textproto/writer.go#L36
		tpconn: textproto.NewConn(conn),
	}

	// Get remote end info for the Received header.
	s.remoteIP, _, _ = net.SplitHostPort(s.conn.RemoteAddr().String())
	names, err := net.LookupAddr(s.remoteIP)
	if err == nil && len(names) > 0 {
		s.remoteHost = names[0]
	} else {
		s.remoteHost = "unknown"
	}

	// Set tls = true if TLS is already in use.
	_, s.tls = s.conn.(*tls.Conn)

	return
}
