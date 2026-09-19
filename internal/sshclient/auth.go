package sshclient

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wanstu/ssh-client/internal/model"
	"golang.org/x/crypto/ssh"
)

type runtimeError struct {
	Code      string
	Stage     string
	Retryable bool
	Err       error
}

func (e *runtimeError) Error() string {
	if e == nil || e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}
func (e *runtimeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func buildAuthMethods(auth model.AuthConfig, creds Credentials) ([]ssh.AuthMethod, []io.Closer, error) {
	var methods []ssh.AuthMethod
	var closers []io.Closer
	var firstAgentErr error

	addAgent := func() {
		signers, closer, err := agentSigners()
		if err != nil {
			if firstAgentErr == nil {
				firstAgentErr = err
			}
			return
		}
		methods = append(methods, ssh.PublicKeys(signers...))
		closers = append(closers, closer)
	}
	addKey := func() error {
		path := strings.TrimSpace(auth.PrivateKeyPath)
		if path == "" {
			return errors.New("private key path is empty")
		}
		expanded, err := expandHome(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(expanded)
		if err != nil {
			return fmt.Errorf("read private key %s: %w", expanded, err)
		}
		var signer ssh.Signer
		if creds.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(creds.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(data)
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				return &runtimeError{Code: "AUTH_KEY_PASSPHRASE_REQUIRED", Stage: "authenticate", Err: errors.New("private key requires a passphrase")}
			}
		}
		if err != nil {
			return fmt.Errorf("parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
		return nil
	}
	addPassword := func() {
		if creds.Password != "" {
			methods = append(methods, ssh.Password(creds.Password))
		}
	}

	switch auth.Mode {
	case "password":
		addPassword()
		if len(methods) == 0 {
			return nil, closers, &runtimeError{Code: "AUTH_PASSWORD_REQUIRED", Stage: "authenticate", Err: errors.New("password is required")}
		}
	case "private_key":
		if err := addKey(); err != nil {
			return nil, closers, wrapAuthError(err)
		}
	case "ssh_agent":
		addAgent()
		if len(methods) == 0 {
			return nil, closers, &runtimeError{Code: "AUTH_AGENT_UNAVAILABLE", Stage: "authenticate", Err: firstAgentErr}
		}
	case "auto":
		addAgent()
		if strings.TrimSpace(auth.PrivateKeyPath) != "" {
			if err := addKey(); err != nil {
				return nil, closers, wrapAuthError(err)
			}
		}
		addPassword()
		if len(methods) == 0 {
			if firstAgentErr != nil {
				return nil, closers, &runtimeError{Code: "AUTH_AGENT_UNAVAILABLE", Stage: "authenticate", Err: firstAgentErr}
			}
			return nil, closers, &runtimeError{Code: "AUTH_METHOD_REJECTED", Stage: "authenticate", Err: errors.New("no authentication method is available")}
		}
	default:
		return nil, closers, &runtimeError{Code: "AUTH_METHOD_REJECTED", Stage: "authenticate", Err: fmt.Errorf("unsupported auth mode %q", auth.Mode)}
	}
	return methods, closers, nil
}

func wrapAuthError(err error) error {
	var coded *runtimeError
	if errors.As(err, &coded) {
		return coded
	}
	return &runtimeError{Code: "AUTH_KEY_REJECTED", Stage: "authenticate", Err: err}
}

func expandHome(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return filepath.Clean(path), nil
}
