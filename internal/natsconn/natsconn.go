// Package natsconn opens the NATS connection every service uses, with the
// optional credentials read from the environment. With none set the
// connection is anonymous, as before.
//
// At most one credential form may be set:
//   - NATS_TOKEN: a shared token (server `authorization { token: ... }`).
//   - NATS_USER and NATS_PASSWORD: both, or neither.
//   - NATS_CREDS: path to a decentralized-auth .creds file (JWT + NKey seed).
//
// A half-set pair or more than one form is an error, so a misconfiguration
// stops the service instead of silently connecting anonymously. Credential
// values never appear in errors or logs.
package natsconn

import (
	"errors"
	"os"

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
	return nats.Connect(url, opts...)
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
