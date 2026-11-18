package skills

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

func TestParseStandaloneSkillMarkdown(t *testing.T) {
	raw := []byte("---\nname: Weekly Report\ndescription: Summarize weekly reports.\nallowed-tools:\n  - reports__list\n  - reports__get\n---\n\nRead the latest report and summarize its key changes.\n")
	parsed, err := ParseUpload("SKILL.md", raw)
	if err != nil {
		t.Fatalf("ParseUpload returned error: %v", err)
	}
	if parsed.Name != "weekly-report" {
		t.Fatalf("name = %q, want weekly-report", parsed.Name)
	}
	if parsed.Description != "Summarize weekly reports." {
		t.Fatalf("description = %q", parsed.Description)
	}
	if parsed.Instructions != "Read the latest report and summarize its key changes." {
		t.Fatalf("instructions = %q", parsed.Instructions)
	}
	if strings.Join(parsed.AllowedTools, ",") != "reports__list,reports__get" {
		t.Fatalf("allowed tools = %v", parsed.AllowedTools)
	}
	if parsed.SourceFormat != "md" || parsed.Filename != "SKILL.md" || parsed.SizeBytes != int64(len(raw)) || parsed.SHA256Hash == "" {
		t.Fatalf("unexpected upload metadata: %+v", parsed)
	}
}

func TestParseSkillArchiveWithReferenceAndMacNoise(t *testing.T) {
	data := makeZip(t, map[string][]byte{
		"team/SKILL.md":             []byte("---\nname: incident-review\ndescription: Review incidents.\nallowed-tools: [alerts__list, alerts__get]\n---\n\nReview active incidents.\n"),
		"team/references/guide.md":  []byte("# Incident review guide\n"),
		"team/examples/sample.json": []byte(`{"severity":"high"}`),
		"__MACOSX/._SKILL.md":       []byte("ignored"),
		"team/.DS_Store":            []byte("ignored"),
	})

	parsed, err := ParseUpload("incident-review.zip", data)
	if err != nil {
		t.Fatalf("ParseUpload returned error: %v", err)
	}
	if parsed.Name != "incident-review" || parsed.SourceFormat != "zip" {
		t.Fatalf("unexpected parsed skill: %+v", parsed)
	}
	if parsed.ReferenceFiles["references/guide.md"] != "# Incident review guide\n" {
		t.Fatalf("reference file not preserved: %#v", parsed.ReferenceFiles)
	}
	if parsed.ReferenceFiles["examples/sample.json"] != `{"severity":"high"}` {
		t.Fatalf("JSON reference file not preserved: %#v", parsed.ReferenceFiles)
	}
	if len(parsed.ReferenceFiles) != 2 {
		t.Fatalf("got %d reference files, want 2", len(parsed.ReferenceFiles))
	}
	if strings.Join(parsed.AllowedTools, ",") != "alerts__list,alerts__get" {
		t.Fatalf("allowed tools = %v", parsed.AllowedTools)
	}
}

func TestParseUploadRejectsInvalidMarkdown(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{name: "missing frontmatter", data: []byte("# No frontmatter\n")},
		{name: "missing name", data: []byte("---\ndescription: Missing name\n---\n\nBody\n")},
		{name: "missing description", data: []byte("---\nname: missing-description\n---\n\nBody\n")},
		{name: "empty body", data: []byte("---\nname: empty-body\ndescription: Has metadata.\n---\n\n")},
		{name: "null byte", data: []byte("---\nname: null-byte\ndescription: Invalid text.\n---\n\nBody\x00\n")},
		{name: "invalid UTF-8", data: []byte("---\nname: invalid-utf8\ndescription: Invalid text.\n---\n\nBody\xff\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseUpload("skill.md", tc.data)
			if !errors.Is(err, ErrInvalidSkill) {
				t.Fatalf("ParseUpload error = %v, want ErrInvalidSkill", err)
			}
		})
	}
}

func TestParseUploadRejectsUnsupportedAndOversizedFiles(t *testing.T) {
	if _, err := ParseUpload("skill.py", []byte("print('no')")); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("unsupported extension error = %v, want ErrInvalidSkill", err)
	}
	if _, err := ParseUpload("skill.zip", []byte("not a zip")); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("invalid zip error = %v, want ErrInvalidSkill", err)
	}
	if _, err := ParseUpload("skill.md", bytes.Repeat([]byte("x"), MaxUploadBytes+1)); !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("oversized markdown error = %v, want ErrUploadTooLarge", err)
	}
}

func TestParseSkillArchiveRejectsUnsafeEntries(t *testing.T) {
	tests := []struct {
		name  string
		files map[string][]byte
	}{
		{
			name: "path traversal",
			files: map[string][]byte{
				"SKILL.md":   []byte("---\nname: safe\ndescription: Safe skill.\n---\n\nBody\n"),
				"../evil.md": []byte("unsafe"),
			},
		},
		{
			name: "binary file",
			files: map[string][]byte{
				"SKILL.md":  []byte("---\nname: safe\ndescription: Safe skill.\n---\n\nBody\n"),
				"image.png": []byte("binary"),
			},
		},
		{
			name: "script file",
			files: map[string][]byte{
				"SKILL.md": []byte("---\nname: safe\ndescription: Safe skill.\n---\n\nBody\n"),
				"run.sh":   []byte("echo unsafe"),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseUpload("bundle.zip", makeZip(t, tc.files)); !errors.Is(err, ErrInvalidSkill) {
				t.Fatalf("ParseUpload error = %v, want ErrInvalidSkill", err)
			}
		})
	}
	executable := makeZipWithExecutable(t, map[string][]byte{
		"SKILL.md": []byte("---\nname: executable\n description: Invalid package.\n---\n\nBody\n"),
		"guide.md": []byte("not executable content but executable mode"),
	})
	if _, err := ParseUpload("executable.zip", executable); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("executable mode error = %v, want ErrInvalidSkill", err)
	}
}

func TestParseSkillArchiveRejectsExpandedSizeAndFileCount(t *testing.T) {
	large := map[string][]byte{
		"SKILL.md":  []byte("---\nname: large\ndescription: Large skill.\n---\n\nBody\n"),
		"large.txt": bytes.Repeat([]byte("a"), MaxExpandedBytes),
	}
	if _, err := ParseUpload("large.zip", makeZip(t, large)); !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("expanded size error = %v, want ErrUploadTooLarge", err)
	}

	many := map[string][]byte{
		"SKILL.md": []byte("---\nname: many\ndescription: Many files.\n---\n\nBody\n"),
	}
	for i := 0; i < MaxBundleFiles; i++ {
		many[fmt.Sprintf("reference-%02d.txt", i)] = []byte("reference")
	}
	if _, err := ParseUpload("many.zip", makeZip(t, many)); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("file count error = %v, want ErrInvalidSkill", err)
	}
}

func makeZip(t *testing.T, files map[string][]byte) []byte {
	return makeZipWithExecutable(t, files)
}

func makeZipWithExecutable(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for filename, content := range files {
		header := &zip.FileHeader{Name: filename, Method: zip.Deflate}
		mode := fs.FileMode(0o644)
		if filename == "guide.md" {
			mode = 0o755
		}
		header.SetMode(mode)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("CreateHeader(%q): %v", filename, err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatalf("write %q: %v", filename, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}
