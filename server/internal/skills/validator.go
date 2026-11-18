package skills

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrEmptyUpload       = errors.New("uploaded skill is empty")
	ErrUploadTooLarge    = errors.New("uploaded skill exceeds maximum allowed size")
	ErrInvalidSkill      = errors.New("invalid skill package")
	skillNamePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	allowedReferenceExts = map[string]bool{
		".md": true, ".markdown": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true,
	}
)

func ParseUpload(filename string, data []byte) (ParsedUpload, error) {
	if len(data) == 0 {
		return ParsedUpload{}, ErrEmptyUpload
	}
	if len(data) > MaxUploadBytes {
		return ParsedUpload{}, ErrUploadTooLarge
	}

	filename = filepath.Base(strings.TrimSpace(filename))
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		parsed, err := parseSkillMarkdown(data)
		if err != nil {
			return ParsedUpload{}, err
		}
		parsed.SourceFormat = "md"
		parsed.Filename = filename
		parsed.SizeBytes = int64(len(data))
		parsed.SHA256Hash = hashBytes(data)
		return parsed, nil
	case ".zip":
		if len(data) < 4 || !bytes.Equal(data[:4], []byte{'P', 'K', 0x03, 0x04}) {
			return ParsedUpload{}, fmt.Errorf("%w: invalid zip signature", ErrInvalidSkill)
		}
		return parseSkillArchive(filename, data)
	default:
		return ParsedUpload{}, fmt.Errorf("%w: file must be .md, .markdown, or .zip", ErrInvalidSkill)
	}
}

func parseSkillMarkdown(data []byte) (ParsedUpload, error) {
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return ParsedUpload{}, fmt.Errorf("%w: skill markdown must be UTF-8 text", ErrInvalidSkill)
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return ParsedUpload{}, fmt.Errorf("%w: missing YAML frontmatter", ErrInvalidSkill)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.SplitN(text, "\n---\n", 2)
	if len(parts) != 2 {
		return ParsedUpload{}, fmt.Errorf("%w: unterminated YAML frontmatter", ErrInvalidSkill)
	}
	metadata, err := parseFrontmatter(parts[0][4:])
	if err != nil {
		return ParsedUpload{}, err
	}
	body := strings.TrimSpace(parts[1])
	if body == "" {
		return ParsedUpload{}, fmt.Errorf("%w: skill instructions are empty", ErrInvalidSkill)
	}
	name, err := normalizeName(metadata["name"])
	if err != nil {
		return ParsedUpload{}, err
	}
	description := strings.TrimSpace(metadata["description"])
	if description == "" {
		return ParsedUpload{}, fmt.Errorf("%w: description is required", ErrInvalidSkill)
	}
	return ParsedUpload{
		Name:           name,
		Description:    description,
		Instructions:   body,
		ReferenceFiles: map[string]string{},
		AllowedTools:   parseAllowedTools(metadata["allowed-tools"] + "\n" + metadata["allowed_tools"]),
	}, nil
}

func parseFrontmatter(raw string) (map[string]string, error) {
	result := make(map[string]string)
	var activeList string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") && activeList != "" {
			result[activeList] += "\n" + strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, fmt.Errorf("%w: malformed YAML frontmatter", ErrInvalidSkill)
		}
		key = strings.TrimSpace(key)
		if key != "name" && key != "description" && key != "allowed-tools" && key != "allowed_tools" {
			activeList = ""
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			activeList = key
			continue
		}
		activeList = ""
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid quoted frontmatter value", ErrInvalidSkill)
			}
			value = decoded
		} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
		} else if comment := strings.Index(value, " #"); comment >= 0 {
			value = strings.TrimSpace(value[:comment])
		}
		result[key] = value
	}
	return result, nil
}

func parseAllowedTools(raw string) []string {
	seen := make(map[string]struct{})
	var tools []string
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '[' || r == ']' || r == '"' || r == '\'' || r == ' '
	}) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		tools = append(tools, item)
	}
	return tools
}

func normalizeName(value string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	name = strings.ReplaceAll(name, " ", "-")
	if !skillNamePattern.MatchString(name) {
		return "", fmt.Errorf("%w: name must be a 1-64 character slug", ErrInvalidSkill)
	}
	return name, nil
}

func parseSkillArchive(filename string, data []byte) (ParsedUpload, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ParsedUpload{}, fmt.Errorf("%w: invalid zip archive", ErrInvalidSkill)
	}

	files := make(map[string][]byte)
	var expandedSize uint64
	fileCount := 0
	for _, entry := range reader.File {
		clean, skip, err := cleanArchivePath(entry.Name)
		if err != nil {
			return ParsedUpload{}, err
		}
		if skip || entry.FileInfo().IsDir() {
			continue
		}
		if entry.Mode()&0o170000 == 0o120000 {
			return ParsedUpload{}, fmt.Errorf("%w: symlinks are not allowed", ErrInvalidSkill)
		}
		if entry.Mode().Perm()&0o111 != 0 {
			return ParsedUpload{}, fmt.Errorf("%w: executable archive entries are not allowed", ErrInvalidSkill)
		}
		ext := strings.ToLower(path.Ext(clean))
		if !allowedReferenceExts[ext] {
			return ParsedUpload{}, fmt.Errorf("%w: unsupported archive file %q", ErrInvalidSkill, clean)
		}
		fileCount++
		if fileCount > MaxBundleFiles {
			return ParsedUpload{}, fmt.Errorf("%w: archive contains too many files", ErrInvalidSkill)
		}
		if entry.UncompressedSize64 > uint64(MaxExpandedBytes) || expandedSize+entry.UncompressedSize64 > uint64(MaxExpandedBytes) {
			return ParsedUpload{}, ErrUploadTooLarge
		}
		expandedSize += entry.UncompressedSize64

		rc, err := entry.Open()
		if err != nil {
			return ParsedUpload{}, fmt.Errorf("%w: failed to read archive entry", ErrInvalidSkill)
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, int64(MaxExpandedBytes)+1))
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil {
			return ParsedUpload{}, fmt.Errorf("%w: failed to read archive entry", ErrInvalidSkill)
		}
		if len(content) > MaxExpandedBytes || bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
			return ParsedUpload{}, fmt.Errorf("%w: archive entries must be bounded UTF-8 text", ErrInvalidSkill)
		}
		files[clean] = content
	}

	skillPath, rootPrefix, err := locateSkillFile(files)
	if err != nil {
		return ParsedUpload{}, err
	}
	parsed, err := parseSkillMarkdown(files[skillPath])
	if err != nil {
		return ParsedUpload{}, err
	}
	parsed.ReferenceFiles = make(map[string]string)
	for filename, content := range files {
		if filename == skillPath || (rootPrefix != "" && !strings.HasPrefix(filename, rootPrefix)) {
			continue
		}
		relative := strings.TrimPrefix(filename, rootPrefix)
		if relative == "SKILL.md" {
			continue
		}
		parsed.ReferenceFiles[relative] = string(content)
	}
	parsed.SourceFormat = "zip"
	parsed.Filename = filename
	parsed.SizeBytes = int64(len(data))
	parsed.SHA256Hash = hashBytes(data)
	return parsed, nil
}

func cleanArchivePath(name string) (string, bool, error) {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.IsAbs(name) {
		return "", false, fmt.Errorf("%w: unsafe archive path", ErrInvalidSkill)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return "", false, fmt.Errorf("%w: unsafe archive path", ErrInvalidSkill)
		}
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false, fmt.Errorf("%w: unsafe archive path", ErrInvalidSkill)
	}
	if strings.HasPrefix(clean, "__MACOSX/") || path.Base(clean) == ".DS_Store" {
		return clean, true, nil
	}
	return clean, false, nil
}

func locateSkillFile(files map[string][]byte) (string, string, error) {
	if _, ok := files["SKILL.md"]; ok {
		return "SKILL.md", "", nil
	}
	var found string
	for name := range files {
		if path.Base(name) != "SKILL.md" {
			continue
		}
		if found != "" {
			return "", "", fmt.Errorf("%w: archive must contain one SKILL.md", ErrInvalidSkill)
		}
		found = name
	}
	if found == "" || strings.Count(found, "/") != 1 {
		return "", "", fmt.Errorf("%w: archive must contain SKILL.md at its root or one top-level folder", ErrInvalidSkill)
	}
	return found, path.Dir(found) + "/", nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
