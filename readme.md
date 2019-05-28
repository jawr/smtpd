# smtpd

An SMTP server package written in Go, in the style of the built-in HTTP server. It meets the minimum requirements specified by RFC 2821 & 5321 minus AUTH support.


## History

Xeoncross/smtpd is based on [Mark Hale's smtpd](https://github.com/mhale/smtpd). The differences can be summarised as:

* Removed Authentication support
* Added streaming message processing via https://github.com/Xeoncross/mimestream
* Moved to [textproto.DotReader](https://golang.org/src/net/textproto/reader.go#L281) instead of manual parsing of `.\r\n`

TODO:

* validation for MIME headers: https://github.com/emersion/maddy/blob/master/submission.go

---

mhale/smtpd is based on [Brad Fitzpatrick's go-smtpd](https://github.com/bradfitz/go-smtpd). The differences can be summarised as:

* A simplified message handler
* Changes made for RFC compliance
* Testing has been added
* Code refactoring
* TLS support
* RCPT handler
* Authentication support

## TLS Support

SMTP over TLS works slightly differently to how you might expect if you are used to the HTTP protocol. Some helpful links for background information are:

* [SSL vs TLS vs STARTTLS](https://www.fastmail.com/help/technical/ssltlsstarttls.html)
* [Opportunistic TLS](https://en.wikipedia.org/wiki/Opportunistic_TLS)
* [RFC 2487: SMTP Service Extension for Secure SMTP over TLS](https://tools.ietf.org/html/rfc2487)
* [func (*Client) StartTLS](https://golang.org/pkg/net/smtp/#Client.StartTLS)

The TLS support has three server configuration options. The bare minimum requirement to enable TLS is to supply certificate and key files as in the TLS example below.

* TLSConfig

This option allows custom TLS configurations such as [requiring strong ciphers](https://cipherli.st/) or using other certificate creation methods. If a certificate file and a key file are supplied to the ConfigureTLS function, the default TLS configuration for Go will be used. The default value for TLSConfig is nil, which disables TLS support.

* TLSRequired

This option sets whether TLS is optional or required. If set to true, the only allowed commands are NOOP, EHLO, STARTTLS and QUIT (as specified in RFC 3207) until the connection is upgraded to TLS i.e. until STARTTLS is issued. This option is ignored if TLS is not configured i.e. if TLSConfig is nil. The default is false.

* TLSListener

This option sets whether the listening socket requires an immediate TLS handshake after connecting. It is equivalent to using HTTPS in web servers, or the now defunct SMTPS on port 465. This option is ignored if TLS is not configured i.e. if TLSConfig is nil. The default is false.

There is also a related package configuration option.

* Debug

This option determines if the data being read from or written to the client will be logged. This may help with debugging when using encrypted connections. The default is false.

## Benchmarks

Server performs well handling 30,000 requests a second with tiny message bodies (not including real network overhead).

    go test -bench=. --benchmem

    goos: darwin
    goarch: amd64
    pkg: github.com/mhale/smtpd
    BenchmarkRawProcessingSequence-8   	   10000	    186928 ns/op	  165258 B/op	     303 allocs/op
    BenchmarkNetSMTP-8                 	    2000	    763322 ns/op	  136343 B/op	     340 allocs/op
    BenchmarkEmersionGoSMTP-8          	    3000	    590131 ns/op	  124527 B/op	     390 allocs/op

Profiling is easy as well

    go test -bench=. --benchmem -cpuprofile profile.out
    go tool pprof profile.out  

2019 - MIT License
