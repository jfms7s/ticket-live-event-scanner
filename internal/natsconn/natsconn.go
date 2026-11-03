// Package natsconn opens the NATS connection every service uses, with the
// optional credentials read from the environment. With none set the
// connection is anonymous, as before.
//
// At most one credential form may be set:
//   - NATS_TOKEN: a shared token (server `authorization { token: ... }`).
//   - NATS_USER and NATS_PASSWORD: both, or neither.
//   - NATS_CREDS: path to a decentralized-auth .creds file (JWT + NKey seed).
//
// A half-set pair, more than one form, or a form set alongside credentials
// embedded in NATS_URL (nats://user:pass@host, which nats.go would use
// instead) is an error, so a misconfiguration stops the service instead of
// silently connecting with the wrong identity. Connect errors have any URL
// userinfo redacted, so a credential never reaches the caller's log line.
package natsconn

import (
	"errors"
	"os"
	"strings"

	"github.com/nats-io/nats.go"
)

// Connect connects to the NATS server at url, naming the connection name
// (shown in the server's connection list) and authenticating with the
// credentials in the environment, if any.
func Connect(url, name string) (*nats.Conn, error) {
	opts, err := optionsFromEnv(name, os.Getenv)
	if err != nil {
		return nil, err
	}
	// opts holds nats.Name plus one credential option when a form is set.
	if len(opts) > 1 && len(urlUserinfo(url)) > 0 {
		return nil, errors.New("NATS_URL carries credentials; drop them or unset NATS_TOKEN, NATS_USER/NATS_PASSWORD and NATS_CREDS")
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, redact(err, url)
	}
	return nc, nil
}

// urlUserinfo returns the userinfo part ("user:pass" or "token") of every
// server in a comma-separated NATS URL list that has one. It works on the raw
// string, so it also finds userinfo in a URL that does not parse.
func urlUserinfo(urls string) []string {
	var out []string
	for _, u := range strings.Split(urls, ",") {
		u = strings.TrimSpace(u)
		if i := strings.Index(u, "://"); i >= 0 {
			u = u[i+3:]
		}
		if i := strings.IndexAny(u, "/?#"); i >= 0 {
			u = u[:i]
		}
		if i := strings.LastIndex(u, "@"); i > 0 {
			out = append(out, u[:i])
		}
	}
	return out
}

// redact returns err with any userinfo of url replaced by "REDACTED".
// nats.go puts the URL into parse errors verbatim.
func redact(err error, url string) error {
	msg := err.Error()
	for _, ui := range urlUserinfo(url) {
		msg = strings.ReplaceAll(msg, ui, "REDACTED")
	}
	if msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

// optionsFromEnv returns the connect options for name from the credential
// variables getenv reports. An empty variable counts as unset.
func optionsFromEnv(name string, getenv func(string) string) ([]nats.Option, error) {
	token := getenv("NATS_TOKEN")
	user, password := getenv("NATS_USER"), getenv("NATS_PASSWORD")
	creds := getenv("NATS_CREDS")

	if (user == "") != (password == "") {
		return nil, errors.New("NATS_USER and NATS_PASSWORD must be set together")
	}
	forms := 0
	for _, set := range []bool{token != "", user != "", creds != ""} {
		if set {
			forms++
		}
	}
	if forms > 1 {
		return nil, errors.New("set only one of NATS_TOKEN, NATS_USER/NATS_PASSWORD or NATS_CREDS")
	}

	opts := []nats.Option{nats.Name(name)}
	switch {
	case token != "":
		opts = append(opts, nats.Token(token))
	case user != "":
		opts = append(opts, nats.UserInfo(user, password))
	case creds != "":
		opts = append(opts, nats.UserCredentials(creds))
	}
	return opts, nil
}
