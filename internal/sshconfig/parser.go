package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Entry struct {
	Alias        string `json:"alias"`
	HostName     string `json:"host_name"`
	User         string `json:"user"`
	Port         int    `json:"port"`
	IdentityFile string `json:"identity_file,omitempty"`
	ProxyJump    string `json:"proxy_jump,omitempty"`
	SourcePath   string `json:"source_path"`
	Supported    bool   `json:"supported"`
	Warning      string `json:"warning,omitempty"`
}

type Preview struct {
	Path     string   `json:"path"`
	Entries  []Entry  `json:"entries"`
	Warnings []string `json:"warnings,omitempty"`
}

type optionState struct {
	hostName        string
	hostNameSet     bool
	user            string
	userSet         bool
	port            int
	portSet         bool
	identityFile    string
	identityFileSet bool
	proxyJump       string
	proxyJumpSet    bool
}

type parsedEntry struct {
	alias      string
	options    optionState
	supported  bool
	warning    string
	sourcePath string
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

func PreviewFile(path string) (Preview, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return Preview{}, err
		}
	}
	expanded, err := expandHome(path)
	if err != nil {
		return Preview{}, err
	}
	data, err := os.ReadFile(expanded)
	if err != nil {
		return Preview{}, fmt.Errorf("读取 SSH Config 失败: %w", err)
	}
	return Parse(string(data), expanded)
}

// Parse builds a conservative import preview. It intentionally does not expand
// Include or Match. OpenSSH's first-obtained-value behaviour is preserved for
// the supported scalar options inside the selected file.
func Parse(content, sourcePath string) (Preview, error) {
	preview := Preview{Path: sourcePath}
	defaults := optionState{port: 22}
	var entries []parsedEntry
	var currentIndexes []int
	seenHost := false
	insideMatch := false

	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := stripComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value := splitDirective(line)
		if key == "" {
			continue
		}
		lower := strings.ToLower(key)

		switch lower {
		case "host":
			seenHost = true
			insideMatch = false
			currentIndexes = nil
			for _, alias := range strings.Fields(value) {
				if !isConcreteAlias(alias) {
					continue
				}
				item := parsedEntry{
					alias:      alias,
					options:    defaults,
					supported:  true,
					sourcePath: sourcePath,
				}
				entries = append(entries, item)
				currentIndexes = append(currentIndexes, len(entries)-1)
			}
			continue
		case "match":
			insideMatch = true
			currentIndexes = nil
			preview.Warnings = appendUnique(preview.Warnings, "Match 块未导入；请在导入后手动核对对应 Host")
			continue
		case "include":
			preview.Warnings = appendUnique(preview.Warnings, "Include 未自动展开；当前预览只读取所选文件")
			continue
		}

		if insideMatch {
			continue
		}

		if !seenHost {
			if err := applyDirective(&defaults, lower, value, lineNo); err != nil {
				return Preview{}, err
			}
			continue
		}

		for _, index := range currentIndexes {
			if lower == "proxycommand" && !strings.EqualFold(strings.TrimSpace(value), "none") {
				entries[index].supported = false
				entries[index].warning = "ProxyCommand 暂不能安全转换为应用连接模型"
				continue
			}
			if err := applyDirective(&entries[index].options, lower, value, lineNo); err != nil {
				return Preview{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Preview{}, err
	}

	preview.Entries = make([]Entry, 0, len(entries))
	for _, item := range entries {
		hostName := item.options.hostName
		if hostName == "" {
			hostName = item.alias
		}
		port := item.options.port
		if port == 0 {
			port = 22
		}
		supported := item.supported
		warning := item.warning
		if strings.Contains(item.options.proxyJump, ",") {
			supported = false
			warning = "多级 ProxyJump 暂不支持；第一阶段只允许一层 Jump Host"
		}
		preview.Entries = append(preview.Entries, Entry{
			Alias:        item.alias,
			HostName:     hostName,
			User:         item.options.user,
			Port:         port,
			IdentityFile: item.options.identityFile,
			ProxyJump:    item.options.proxyJump,
			SourcePath:   item.sourcePath,
			Supported:    supported,
			Warning:      warning,
		})
	}
	return preview, nil
}

func applyDirective(state *optionState, key, value string, lineNo int) error {
	switch key {
	case "hostname":
		if !state.hostNameSet {
			state.hostName = unquote(value)
			state.hostNameSet = true
		}
	case "user":
		if !state.userSet {
			state.user = unquote(value)
			state.userSet = true
		}
	case "port":
		if !state.portSet {
			port, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("line %d: invalid port %q", lineNo, value)
			}
			state.port = port
			state.portSet = true
		}
	case "identityfile":
		if !state.identityFileSet {
			state.identityFile = unquote(value)
			state.identityFileSet = true
		}
	case "proxyjump":
		if !state.proxyJumpSet {
			if !strings.EqualFold(strings.TrimSpace(value), "none") {
				state.proxyJump = unquote(value)
			}
			state.proxyJumpSet = true
		}
	}
	return nil
}

func splitDirective(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	for i, r := range trimmed {
		if r == ' ' || r == '\t' || r == '=' {
			return strings.TrimSpace(trimmed[:i]), strings.TrimSpace(strings.TrimLeft(trimmed[i:], " \t="))
		}
	}
	return trimmed, ""
}

func stripComment(line string) string {
	quoted := false
	for i, r := range line {
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == '#' && !quoted {
			return line[:i]
		}
	}
	return line
}

func unquote(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"'")
}

func isConcreteAlias(alias string) bool {
	return alias != "" && !strings.ContainsAny(alias, "*?!")
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
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
