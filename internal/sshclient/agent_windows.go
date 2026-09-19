//go:build windows

package sshclient

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func agentSigners() ([]ssh.Signer, io.Closer, error) {
	pipe := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK"))
	if pipe == "" {
		pipe = `\\.\pipe\openssh-ssh-agent`
	}
	if !strings.HasPrefix(strings.ToLower(pipe), `\\.\pipe\`) {
		return nil, nil, fmt.Errorf("unsupported Windows SSH agent endpoint %q", pipe)
	}
	timeout := 2 * time.Second
	conn, err := winio.DialPipe(pipe, &timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("connect SSH agent %s: %w", pipe, err)
	}
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("read SSH agent identities: %w", err)
	}
	if len(signers) == 0 {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("SSH agent has no identities")
	}
	return signers, conn, nil
}
