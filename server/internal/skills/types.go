package skills

import "time"

const (
	MaxUploadBytes     = 2 * 1024 * 1024
	MaxExpandedBytes   = 4 * 1024 * 1024
	MaxBundleFiles     = 32
	MaxSkillNameLength = 64
)

type Skill struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Instructions   string            `json:"instructions"`
	ReferenceFiles map[string]string `json:"referenceFiles,omitempty"`
	AllowedTools   []string          `json:"allowedTools"`
	SourceFormat   string            `json:"sourceFormat"`
	Filename       string            `json:"filename"`
	SizeBytes      int64             `json:"sizeBytes"`
	SHA256Hash     string            `json:"sha256Hash"`
	Enabled        bool              `json:"enabled"`
	UploadedBy     *string           `json:"uploadedBy,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

type ParsedUpload struct {
	Name           string
	Description    string
	Instructions   string
	ReferenceFiles map[string]string
	AllowedTools   []string
	SourceFormat   string
	Filename       string
	SizeBytes      int64
	SHA256Hash     string
}
