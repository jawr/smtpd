package smtpd

import (
	"io"
	"io/ioutil"
	"net/smtp"
	"testing"

	esmtp "github.com/emersion/go-smtp"
)

//
// github.com/emersion is perhaps the best Go + Email developer in the world.
// He has written and produced more email-related libraries than anyone else
// and has done a great job as well; nice abstractions, streaming
// io.Reader/Writer interfaces, and good performance.
// His code is worth a look.
//

type EmersionBackend struct{}

func (bkd *EmersionBackend) Login(state *esmtp.ConnectionState, username, password string) (esmtp.Session, error) {
	return &EmersionSession{}, nil
}

func (bkd *EmersionBackend) AnonymousLogin(state *esmtp.ConnectionState) (esmtp.Session, error) {
	return &EmersionSession{}, nil
}

type EmersionSession struct{}

func (s *EmersionSession) Mail(from string) error {
	return nil
}

func (s *EmersionSession) Rcpt(to string) error {
	return nil
}

func (s *EmersionSession) Data(r io.Reader) error {
	_, err := io.Copy(ioutil.Discard, r)
	return err
}

func (s *EmersionSession) Reset() {}

func (s *EmersionSession) Logout() error {
	return nil
}

func TestEmersionGoSMTP(t *testing.T) {

	addr, err := PickRandomPort()
	if err != nil {
		t.Fatal(err)
	}

	s := esmtp.NewServer(&EmersionBackend{})

	s.Addr = addr
	s.Domain = "localhost"
	// s.ReadTimeout = 10 * time.Second
	// s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50

	go func() {
		_ = s.ListenAndServe()
	}()

	to := []string{"recipient@example.com"}
	msg, err := ioutil.ReadAll(SampleEmailBody())
	if err != nil {
		t.Fatal(err)
	}

	err = smtp.SendMail(addr, nil, "sender@example.com", to, msg)
	if err != nil {
		t.Fatal(err)
	}

}

func BenchmarkEmersionGoSMTP(b *testing.B) {

	addr, err := PickRandomPort()
	if err != nil {
		b.Fatal(err)
	}

	s := esmtp.NewServer(&EmersionBackend{})

	s.Addr = addr
	s.Domain = "localhost"
	// s.ReadTimeout = 10 * time.Second
	// s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50

	go func() {
		_ = s.ListenAndServe()
	}()

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
	}
}
