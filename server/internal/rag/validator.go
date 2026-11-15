package rag

import (
	"archive/zip"
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const DefaultMaxUploadBytes int64 = 20 * 1024 * 1024 // 20 MB

type DocType string

const (
	DocTypePDF  DocType = "pdf"
	DocTypeDOCX DocType = "docx"
	DocTypeMD   DocType = "md"
	DocTypeTXT  DocType = "txt"
)

var (
	ErrEmptyFile       = errors.New("uploaded file is empty")
	ErrPayloadTooLarge = errors.New("uploaded file exceeds maximum allowed size")
	ErrUnsupportedType = errors.New("unsupported or spoofed file type")
)

func ValidateUpload(filename string, data []byte, maxBytes int64) (DocType, string, error) {
	if len(data) == 0 {
		return "", "", ErrEmptyFile
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxUploadBytes
	}
	if int64(len(data)) > maxBytes {
		return "", "", ErrPayloadTooLarge
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return "", "", ErrUnsupportedType
		}
		return DocTypePDF, "application/pdf", nil

	case ".docx":
		if len(data) < 4 || !bytes.Equal(data[:4], []byte{'P', 'K', 0x03, 0x04}) {
			return "", "", ErrUnsupportedType
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", "", ErrUnsupportedType
		}
		hasDocXML := false
		for _, f := range zr.File {
			if f.Name == "word/document.xml" {
				hasDocXML = true
				break
			}
		}
		if !hasDocXML {
			return "", "", ErrUnsupportedType
		}
		return DocTypeDOCX, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil

	case ".md", ".markdown":
		if bytes.IndexByte(data, 0) != -1 || !utf8.Valid(data) {
			return "", "", ErrUnsupportedType
		}
		return DocTypeMD, "text/markdown", nil

	case ".txt":
		if bytes.IndexByte(data, 0) != -1 || !utf8.Valid(data) {
			return "", "", ErrUnsupportedType
		}
		return DocTypeTXT, "text/plain", nil

	default:
		return "", "", ErrUnsupportedType
	}
}
