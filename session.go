package smtpd

import (
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/Xeoncross/mimestream"
)

type session struct {
	srv    *Server
	conn   net.Conn
	tpconn *textproto.Conn

	remoteIP   string // Remote IP address
	remoteHost string // Remote hostname according to reverse DNS lookup
	remoteName string // Remote hostname as supplied with EHLO
	tls        bool
}

// Function called to handle connection requests.
func (s *session) serve() {
	defer s.conn.Close()
	var from string
	var gotFrom bool
	var to []string

	// Send banner.
	s.writef("220 %s %s ESMTP Service ready", s.srv.Hostname, s.srv.Appname)

loop:
	for {
		// Attempt to read a line from the socket.
		// On timeout, send a timeout message and return from serve().
		// On error, assume the client has gone away i.e. return from serve().

		line, err := s.readLine()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				s.writef("421 4.4.2 %s %s ESMTP Service closing transmission channel after timeout exceeded", s.srv.Hostname, s.srv.Appname)
			}
			break
		}
		verb, args := s.parseLine(line)

		switch verb {
		case "HELO":
			s.remoteName = args
			s.writef("250 %s greets %s", s.srv.Hostname, s.remoteName)

			// RFC 2821 section 4.1.4 specifies that EHLO has the same effect as RSET, so reset for HELO too.
			from = ""
			gotFrom = false
			to = nil
		case "EHLO":
			s.remoteName = args
			s.writef(s.makeEHLOResponse())

			// RFC 2821 section 4.1.4 specifies that EHLO has the same effect as RSET.
			from = ""
			gotFrom = false
			to = nil
		case "MAIL":
			if s.srv.TLSConfig != nil && s.srv.TLSRequired && !s.tls {
				s.writef("530 5.7.0 Must issue a STARTTLS command first")
				break
			}

			match := mailFromRE.FindStringSubmatch(args)
			if match == nil {
				s.writef("501 5.5.4 Syntax error in parameters or arguments (invalid FROM parameter)")
			} else {
				// Validate the SIZE parameter if one was sent.
				if len(match[2]) > 0 { // A parameter is present
					sizeMatch := mailSizeRE.FindStringSubmatch(match[3])
					if sizeMatch == nil {
						s.writef("501 5.5.4 Syntax error in parameters or arguments (invalid SIZE parameter)")
					} else {
						// Enforce the maximum message size if one is set.
						size, err := strconv.Atoi(sizeMatch[1])
						if err != nil { // Bad SIZE parameter
							s.writef("501 5.5.4 Syntax error in parameters or arguments (invalid SIZE parameter)")
						} else if s.srv.MaxSize > 0 && size > s.srv.MaxSize { // SIZE above maximum size, if set
							err = maxSizeExceeded(s.srv.MaxSize)
							s.writef(err.Error())
						} else { // SIZE ok
							from = match[1]
							gotFrom = true
							s.writef("250 2.1.0 Ok")
						}
					}
				} else { // No parameters after FROM
					from = match[1]
					gotFrom = true
					s.writef("250 2.1.0 Ok")
				}
			}
			to = nil
			// buffer.Reset()
		case "RCPT":
			if s.srv.TLSConfig != nil && s.srv.TLSRequired && !s.tls {
				s.writef("530 5.7.0 Must issue a STARTTLS command first")
				break
			}

			if !gotFrom {
				s.writef("503 5.5.1 Bad sequence of commands (MAIL required before RCPT)")
				break
			}

			match := rcptToRE.FindStringSubmatch(args)
			if match == nil {
				s.writef("501 5.5.4 Syntax error in parameters or arguments (invalid TO parameter)")
			} else {
				// RFC 5321 specifies 100 minimum recipients
				// https://tools.ietf.org/html/rfc5321#section-4.5.3.1.10
				if len(to) == 100 {
					s.writef("452 4.5.3 Too many recipients")
				} else {
					accept := true
					if s.srv.HandlerRcpt != nil {
						accept = s.srv.HandlerRcpt(s.conn.RemoteAddr(), from, match[1])
					}
					if accept {
						to = append(to, match[1])
						s.writef("250 2.1.5 Ok")
					} else {
						s.writef("550 5.1.0 Requested action not taken: mailbox unavailable")
					}
				}
			}
		case "DATA":
			if s.srv.TLSConfig != nil && s.srv.TLSRequired && !s.tls {
				s.writef("530 5.7.0 Must issue a STARTTLS command first")
				break
			}

			if !gotFrom || len(to) == 0 {
				s.writef("503 5.5.1 Bad sequence of commands (MAIL & RCPT required before DATA)")
				break
			}

			s.writef("354 Start mail input; end with <CR><LF>.<CR><LF>")

			// Regardless of the limit desired, this is useful to track how much we
			// have already read in the handler
			r := &MaxReader{Reader: s.tpconn.DotReader(), MaxBytes: s.srv.MaxSize}

			// Create Received header & write message body into buffer.
			// buffer.Write(s.makeHeaders(to))

			err = mimestream.HandleEmailFromReader(r, func(header textproto.MIMEHeader, body io.Reader) (err error) {

				// Pass mail on to handler.
				if s.srv.Handler != nil {
					err = s.srv.Handler(r.BytesRead, s.conn.RemoteAddr(), from, to, header, body)
				} else if Debug {
					var b []byte
					b, err = ioutil.ReadAll(body)

					fmt.Printf("\nMaxReader Read: %d with limit %d\n", r.BytesRead, s.srv.MaxSize)
					fmt.Printf("HEADER: %v\n", header)
					fmt.Printf("BODY: %d %q\n", len(b), b)

					if err != nil {
						return err
					}
				}

				// Read any remaining body to trigger maxSizeExceeded if needed
				if err == nil {
					_, err = io.Copy(ioutil.Discard, body)
				}

				return
			})

			if err != nil {
				switch err.(type) {
				case net.Error:
					if err.(net.Error).Timeout() {
						s.writef("421 4.4.2 %s %s ESMTP Service closing transmission channel after timeout exceeded", s.srv.Hostname, s.srv.Appname)
					}
					break loop
				case maxSizeExceededError:
					s.writef(err.Error())
					continue
				default:
					// s.writef("451 4.3.0 Requested action aborted: local error in processing")
					s.writef("451 4.3.0 Requested action aborted: " + err.Error())
					continue
				}
			}

			// Read anything left
			// io.Copy(ioutil.Discard, r)

			// Mail processing complete
			if s.srv.HandlerSuccess != nil {
				s.srv.HandlerSuccess(r.BytesRead, s.conn.RemoteAddr(), from, to)
			}

			s.writef("250 2.0.0 Ok: queued")

			// Reset for next mail.
			from = ""
			gotFrom = false
			to = nil
		case "QUIT":
			s.writef("221 2.0.0 %s %s ESMTP Service closing transmission channel", s.srv.Hostname, s.srv.Appname)
			break loop
		case "RSET":
			if s.srv.TLSConfig != nil && s.srv.TLSRequired && !s.tls {
				s.writef("530 5.7.0 Must issue a STARTTLS command first")
				break
			}
			s.writef("250 2.0.0 Ok")
			from = ""
			gotFrom = false
			to = nil
		case "NOOP":
			s.writef("250 2.0.0 Ok")
		case "HELP", "VRFY", "EXPN":
			// See RFC 5321 section 4.2.4 for usage of 500 & 502 response codes.
			s.writef("502 5.5.1 Command not implemented")
		case "STARTTLS":
			// Parameters are not allowed (RFC 3207 section 4).
			if args != "" {
				s.writef("501 5.5.2 Syntax error (no parameters allowed)")
				break
			}

			// Handle case where TLS is requested but not configured (and therefore not listed as a service extension).
			if s.srv.TLSConfig == nil {
				s.writef("502 5.5.1 Command not implemented")
				break
			}

			// Handle case where STARTTLS is received when TLS is already in use.
			if s.tls {
				s.writef("503 5.5.1 Bad sequence of commands (TLS already in use)")
				break
			}

			s.writef("220 2.0.0 Ready to start TLS")

			// Establish a TLS connection with the client.
			tlsConn := tls.Server(s.conn, s.srv.TLSConfig)
			err := tlsConn.Handshake()
			if err != nil {
				s.writef("403 4.7.0 TLS handshake failed")
				break
			}

			// TLS handshake succeeded, switch to using the TLS connection.
			s.conn = tlsConn
			s.tpconn = textproto.NewConn(tlsConn)
			s.tls = true

			// RFC 3207 specifies that the server must discard any prior knowledge obtained from the client.
			s.remoteName = ""
			from = ""
			gotFrom = false
			to = nil
		case "AUTH":

			// RFC 4954 also specifies that ESMTP code 5.5.4 ("Invalid command arguments")
			// should be returned when attempting to use an unsupported authentication type.
			// Many servers return 5.7.4 ("Security features not supported") instead.
			// RFC 4954 specifies that AUTH is not permitted during mail transactions.

			// None of this matters for us though, we don't use standard user/pass AUTH
			s.writef("502 5.5.1 Command not implemented")
			break

		default:
			// See RFC 5321 section 4.2.4 for usage of 500 & 502 response codes.
			s.writef("500 5.5.2 Syntax error, command unrecognized")
		}
	}
}

// Wrapper function for writing a complete line to the socket.
func (s *session) writef(format string, args ...interface{}) (err error) {
	if s.srv.Timeout > 0 {
		err = s.conn.SetWriteDeadline(time.Now().Add(s.srv.Timeout))
		if err != nil {
			return
		}
	}

	err = s.tpconn.Writer.PrintfLine(format, args...)

	if Debug {
		line := fmt.Sprintf(format, args...)
		verb := "WROTE"
		if s.srv.LogWrite != nil {
			s.srv.LogWrite(s.remoteIP, verb, line)
		} else {
			log.Println(s.remoteIP, verb, line)
		}
	}

	return
}

// Read a complete line from the socket.
func (s *session) readLine() (line string, err error) {
	if s.srv.Timeout > 0 {
		err = s.conn.SetReadDeadline(time.Now().Add(s.srv.Timeout))
		if err != nil {
			return
		}
	}

	line, err = s.tpconn.ReadLine()

	if Debug {
		verb := "READ"
		if s.srv.LogRead != nil {
			s.srv.LogRead(s.remoteIP, verb, line)
		} else {
			log.Println(s.remoteIP, verb, line)
		}
	}

	return
}

// Parse a line read from the socket.
func (s *session) parseLine(line string) (verb string, args string) {
	if idx := strings.Index(line, " "); idx != -1 {
		verb = strings.ToUpper(line[:idx])
		args = strings.TrimSpace(line[idx+1:])
	} else {
		verb = strings.ToUpper(line)
		args = ""
	}
	return verb, args
}

// Create the Received header to comply with RFC 2821 section 3.8.2.
// TODO: Work out what to do with multiple to addresses.
// func (s *session) makeHeaders(to []string) []byte {
// 	var buffer bytes.Buffer
// 	now := time.Now().Format("Mon, _2 Jan 2006 15:04:05 -0700 (MST)")
// 	buffer.WriteString(fmt.Sprintf("Received: from %s (%s [%s])\r\n", s.remoteName, s.remoteHost, s.remoteIP))
// 	buffer.WriteString(fmt.Sprintf("        by %s (%s) with SMTP\r\n", s.srv.Hostname, s.srv.Appname))
// 	buffer.WriteString(fmt.Sprintf("        for <%s>; %s\r\n", to[0], now))
// 	return buffer.Bytes()
// }

// Create the greeting string sent in response to an EHLO command.
func (s *session) makeEHLOResponse() (response string) {
	response = fmt.Sprintf("250-%s greets %s\r\n", s.srv.Hostname, s.remoteName)

	// RFC 1870 specifies that "SIZE 0" indicates no maximum size is in force.
	response += fmt.Sprintf("250-SIZE %d\r\n", s.srv.MaxSize)

	// Only list STARTTLS if TLS is configured, but not currently in use.
	if s.srv.TLSConfig != nil && !s.tls {
		response += "250-STARTTLS\r\n"
	}

	response += "250 ENHANCEDSTATUSCODES"
	return
}
