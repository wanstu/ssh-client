package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
func TestPreviewFileExpandsNestedIncludes(t *testing.T) {
	dir := t.TempDir()
	confDir := filepath.Join(dir, "conf.d")
	nestedDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(dir, "config")
	jumpPath := filepath.Join(confDir, "10-jump.conf")
	targetPath := filepath.Join(confDir, "20-target.conf")
	extraPath := filepath.Join(nestedDir, "extra.conf")
	writeConfigFile(t, root, "User global-user\nInclude conf.d/*.conf\nHost root\n  HostName root.internal\n")
	writeConfigFile(t, jumpPath, "Host jump\n  HostName 10.0.0.10\n")
	writeConfigFile(t, targetPath, "Host target\n  HostName 10.0.0.20\n  ProxyJump jump\nInclude nested/extra.conf\n")
	writeConfigFile(t, extraPath, "Host extra\n  HostName 10.0.0.30\n")

	preview, err := PreviewFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", preview.Warnings)
	}
	if len(preview.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d: %#v", len(preview.Entries), preview.Entries)
	}

	expected := []struct {
		alias  string
		source string
	}{
		{"jump", jumpPath},
		{"target", targetPath},
		{"extra", extraPath},
		{"root", root},
	}
	for i, want := range expected {
		got := preview.Entries[i]
		if got.Alias != want.alias {
			t.Fatalf("entry %d alias = %q, want %q", i, got.Alias, want.alias)
		}
		if filepath.Clean(got.SourcePath) != filepath.Clean(want.source) {
			t.Fatalf("entry %q source = %q, want %q", got.Alias, got.SourcePath, want.source)
		}
		if got.User != "global-user" {
			t.Fatalf("entry %q user = %q, want inherited global-user", got.Alias, got.User)
		}
	}
	if preview.Entries[1].ProxyJump != "jump" {
		t.Fatalf("target ProxyJump = %q", preview.Entries[1].ProxyJump)
	}
}

func TestPreviewFileSkipsDuplicateCycleAndWarnsUnmatched(t *testing.T) {
	dir := t.TempDir()
	confDir := filepath.Join(dir, "conf.d")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(dir, "config")
	aPath := filepath.Join(confDir, "a.conf")
	bPath := filepath.Join(confDir, "b.conf")
	writeConfigFile(t, root, "Include conf.d/a.conf conf.d/*.conf missing/*.conf\nHost root\n  HostName root.internal\n")
	writeConfigFile(t, aPath, "Include config\nHost a\n  HostName a.internal\n")
	writeConfigFile(t, bPath, "Host b\n  HostName b.internal\n")

	preview, err := PreviewFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Entries) != 3 {
		t.Fatalf("expected de-duplicated a,b,root entries, got %#v", preview.Entries)
	}
	aliases := []string{preview.Entries[0].Alias, preview.Entries[1].Alias, preview.Entries[2].Alias}
	if strings.Join(aliases, ",") != "a,b,root" {
		t.Fatalf("unexpected aliases: %#v", aliases)
	}
	if !warningContains(preview.Warnings, "重复或循环 Include") {
		t.Fatalf("expected duplicate/cycle warning, got %#v", preview.Warnings)
	}
	if !warningContains(preview.Warnings, "Include 未匹配任何文件") {
		t.Fatalf("expected unmatched Include warning, got %#v", preview.Warnings)
	}
}

func TestSplitIncludePatternsSupportsQuotedPaths(t *testing.T) {
	got, err := splitIncludePatterns("\"conf dir/*.conf\" plain.conf 'other dir/file.conf'")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"conf dir/*.conf", "plain.conf", "other dir/file.conf"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("patterns = %#v, want %#v", got, want)
	}
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func warningContains(warnings []string, part string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, part) {
			return true
		}
	}
	return false
}
