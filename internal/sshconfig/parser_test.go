package sshconfig

import "testing"

func TestParsePreview(t *testing.T) {
	content := `
User global-user
Port 2200

Host bastion-prod
  HostName 10.0.0.10
  IdentityFile ~/.ssh/bastion

Host prod-web
  HostName 10.0.1.20
  User root
  ProxyJump bastion-prod

Host *.example.com
  User ignored

Host unsupported
  HostName 10.0.9.9
  ProxyCommand ssh proxy nc %h %p
`

	preview, err := Parse(content, "C:/Users/test/.ssh/config")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Entries) != 3 {
		t.Fatalf("expected 3 concrete entries, got %d: %#v", len(preview.Entries), preview.Entries)
	}

	bastion := preview.Entries[0]
	if bastion.Alias != "bastion-prod" || bastion.HostName != "10.0.0.10" || bastion.User != "global-user" || bastion.Port != 2200 {
		t.Fatalf("unexpected bastion: %#v", bastion)
	}
	if bastion.IdentityFile != "~/.ssh/bastion" || !bastion.Supported {
		t.Fatalf("unexpected bastion key/support: %#v", bastion)
	}

	target := preview.Entries[1]
	if target.Alias != "prod-web" || target.User != "global-user" {
		t.Fatalf("OpenSSH first-value semantics not preserved: %#v", target)
	}
	if target.ProxyJump != "bastion-prod" || !target.Supported {
		t.Fatalf("unexpected target: %#v", target)
	}

	unsupported := preview.Entries[2]
	if unsupported.Supported || unsupported.Warning == "" {
		t.Fatalf("expected ProxyCommand entry to be unsupported: %#v", unsupported)
	}
}

func TestParseWarnsForIncludeMatchAndMultiProxyJump(t *testing.T) {
	content := `
Include ~/.ssh/conf.d/*
Host app
  HostName app.internal
  User deploy
  ProxyJump jump-a,jump-b
Match host app
  User ignored
`
	preview, err := Parse(content, "/tmp/config")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Warnings) != 2 {
		t.Fatalf("expected Include and Match warnings, got %#v", preview.Warnings)
	}
	if len(preview.Entries) != 1 || preview.Entries[0].Supported || preview.Entries[0].Warning == "" {
		t.Fatalf("expected multi-hop ProxyJump to be unsupported: %#v", preview.Entries)
	}
}

func TestParseKeepsFirstValueWithinHost(t *testing.T) {
	content := `
Host server
  User first
  User second
  Port 22
  Port 2222
`
	preview, err := Parse(content, "config")
	if err != nil {
		t.Fatal(err)
	}
	if got := preview.Entries[0]; got.User != "first" || got.Port != 22 {
		t.Fatalf("unexpected first-value result: %#v", got)
	}
}

func TestParseRejectsInvalidPort(t *testing.T) {
	_, err := Parse("Host a\n  Port nope\n", "config")
	if err == nil {
		t.Fatal("expected invalid port error")
	}
}
