package sshconfig

import (
	"bufio"
	"errors"
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

const (
	maxIncludeDepth = 16
	maxIncludeFiles = 256
	maxIncludeBytes = 4 * 1024 * 1024
)

type sourceLine struct {
	text       string
	sourcePath string
	lineNo     int
}

type includeLoader struct {
	rootDir    string
	loaded     map[string]struct{}
	totalBytes int64
	warnings   []string
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
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return Preview{}, err
	}
	absolute = filepath.Clean(absolute)

	loader := &includeLoader{
		rootDir: filepath.Dir(absolute),
		loaded:  map[string]struct{}{},
	}
	lines, err := loader.loadFile(absolute, 0, true)
	if err != nil {
		return Preview{}, err
	}
	preview, err := parseLines(lines, absolute, true)
	if err != nil {
		return Preview{}, err
	}
	for _, warning := range loader.warnings {
		preview.Warnings = appendUnique(preview.Warnings, warning)
	}
	return preview, nil
}

func (l *includeLoader) loadFile(path string, depth int, required bool) ([]sourceLine, error) {
	if depth > maxIncludeDepth {
		return nil, fmt.Errorf("SSH Config Include 超过最大深度 %d", maxIncludeDepth)
	}

	absolute, err := l.resolvePath(path)
	if err != nil {
		return nil, err
	}
	key := absolute
	if os.PathSeparator == '\\' {
		key = strings.ToLower(key)
	}
	if _, ok := l.loaded[key]; ok {
		l.warnings = appendUnique(l.warnings, "重复或循环 Include 已跳过: "+absolute)
		return nil, nil
	}
	if len(l.loaded) >= maxIncludeFiles {
		return nil, fmt.Errorf("SSH Config Include 文件数超过上限 %d", maxIncludeFiles)
	}

	info, err := os.Stat(absolute)
	if err != nil {
		if required {
			return nil, fmt.Errorf("读取 SSH Config 失败: %w", err)
		}
		l.warnings = appendUnique(l.warnings, "无法读取 Include 文件: "+absolute)
		return nil, nil
	}
	if info.IsDir() {
		if required {
			return nil, fmt.Errorf("SSH Config 路径是目录: %s", absolute)
		}
		l.warnings = appendUnique(l.warnings, "Include 指向目录，已跳过: "+absolute)
		return nil, nil
	}
	if info.Size() < 0 || l.totalBytes+info.Size() > maxIncludeBytes {
		return nil, fmt.Errorf("SSH Config Include 总读取量超过上限 %d MiB", maxIncludeBytes/(1024*1024))
	}

	data, err := os.ReadFile(absolute)
	if err != nil {
		if required {
			return nil, fmt.Errorf("读取 SSH Config 失败: %w", err)
		}
		l.warnings = appendUnique(l.warnings, "无法读取 Include 文件: "+absolute)
		return nil, nil
	}
	l.loaded[key] = struct{}{}
	l.totalBytes += int64(len(data))

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lines := make([]sourceLine, 0, 64)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		keyName, value := splitDirective(stripComment(raw))
		if !strings.EqualFold(keyName, "include") {
			lines = append(lines, sourceLine{text: raw, sourcePath: absolute, lineNo: lineNo})
			continue
		}

		patterns, err := splitIncludePatterns(value)
		if err != nil {
			l.warnings = appendUnique(l.warnings, fmt.Sprintf("%s:%d Include 无法解析: %v", absolute, lineNo, err))
			continue
		}
		if len(patterns) == 0 {
			l.warnings = appendUnique(l.warnings, fmt.Sprintf("%s:%d Include 为空，已跳过", absolute, lineNo))
			continue
		}

		for _, pattern := range patterns {
			resolved, err := l.resolveIncludePattern(pattern)
			if err != nil {
				l.warnings = appendUnique(l.warnings, fmt.Sprintf("%s:%d Include %q 无法解析: %v", absolute, lineNo, pattern, err))
				continue
			}
			matches, err := filepath.Glob(resolved)
			if err != nil {
				l.warnings = appendUnique(l.warnings, fmt.Sprintf("%s:%d Include glob %q 无效: %v", absolute, lineNo, pattern, err))
				continue
			}
			if len(matches) == 0 {
				l.warnings = appendUnique(l.warnings, fmt.Sprintf("%s:%d Include 未匹配任何文件: %s", absolute, lineNo, pattern))
				continue
			}
			for _, match := range matches {
				child, err := l.loadFile(match, depth+1, false)
				if err != nil {
					return nil, err
				}
				lines = append(lines, child...)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 SSH Config 失败: %w", err)
	}
	return lines, nil
}

func (l *includeLoader) resolvePath(path string) (string, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(l.rootDir, expanded)
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func (l *includeLoader) resolveIncludePattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", errors.New("empty include pattern")
	}
	if strings.Contains(pattern, "%d") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		pattern = strings.ReplaceAll(pattern, "%d", home)
	}
	return l.resolvePath(pattern)
}

func splitIncludePatterns(value string) ([]string, error) {
	var result []string
	var token strings.Builder
	var quote rune
	flush := func() {
		if token.Len() == 0 {
			return
		}
		result = append(result, token.String())
		token.Reset()
	}
	for _, r := range strings.TrimSpace(value) {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		default:
			token.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, errors.New("unclosed quote")
	}
	flush()
	return result, nil
}

func sourceLinesFromContent(content, sourcePath string) ([]sourceLine, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lines := make([]sourceLine, 0, 64)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		lines = append(lines, sourceLine{text: scanner.Text(), sourcePath: sourcePath, lineNo: lineNo})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// Parse builds a conservative import preview. It intentionally does not expand
// Include or Match. OpenSSH's first-obtained-value behaviour is preserved for
// the supported scalar options inside the selected file.
func Parse(content, sourcePath string) (Preview, error) {
	lines, err := sourceLinesFromContent(content, sourcePath)
	if err != nil {
		return Preview{}, err
	}
	return parseLines(lines, sourcePath, false)
}

func parseLines(lines []sourceLine, sourcePath string, includesExpanded bool) (Preview, error) {
	preview := Preview{Path: sourcePath}
	defaults := optionState{port: 22}
	var entries []parsedEntry
	var currentIndexes []int
	seenHost := false
	insideMatch := false

	for _, source := range lines {
		lineNo := source.lineNo
		line := stripComment(source.text)
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
					sourcePath: source.sourcePath,
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
			if !includesExpanded {
				preview.Warnings = appendUnique(preview.Warnings, "Include 未自动展开；当前预览只读取所选文件")
			}
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
