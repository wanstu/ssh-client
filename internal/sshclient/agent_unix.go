//go:build !windows

package sshclient

import (
	"fmt"
	"io"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func agentSigners() ([]ssh.Signer, io.Closer, error) {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, nil, fmt.Errorf("SSH_AUTH_SOCK is not set")
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, nil, fmt.Errorf("connect SSH agent: %w", err)
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
