package rag

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"
)

var ErrExtractionFailed = errors.New("failed to extract text from document")

func ExtractText(docType DocType, data []byte) (string, error) {
	switch docType {
	case DocTypeMD, DocTypeTXT:
		return normalizeWhitespace(string(data)), nil
	case DocTypeDOCX:
		return extractDOCX(data)
	case DocTypePDF:
		return extractPDF(data)
	default:
		return "", ErrUnsupportedType
	}
}

func extractDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", ErrExtractionFailed
	}
	var docFile *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", ErrExtractionFailed
	}
	rc, err := docFile.Open()
	if err != nil {
		return "", ErrExtractionFailed
	}
	defer func() { _ = rc.Close() }()

	dec := xml.NewDecoder(io.LimitReader(rc, 50*1024*1024))
	var out strings.Builder
	var inText bool
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", ErrExtractionFailed
		}
		switch el := tok.(type) {
		case xml.StartElement:
			if el.Name.Local == "t" {
				inText = true
			} else if el.Name.Local == "p" {
				if out.Len() > 0 {
					out.WriteByte('\n')
				}
			}
		case xml.EndElement:
			if el.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				out.Write(el)
			}
		}
	}
	return normalizeWhitespace(out.String()), nil
}

var (
	pdfStreamRegex = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	pdfTextOpRegex = regexp.MustCompile(`\(([^()\\]*(?:\\.[^()\\]*)*)\)\s*Tj|\[(.*?)\]\s*TJ`)
	pdfArrayStr    = regexp.MustCompile(`\(([^()\\]*(?:\\.[^()\\]*)*)\)`)
)

func extractPDF(data []byte) (string, error) {
	var collected strings.Builder

	// Try parsing text operators from raw and zlib-decompressed streams
	matches := pdfStreamRegex.FindAllSubmatch(data, -1)
	for _, m := range matches {
		rawStream := m[1]
		content := rawStream
		if zr, err := zlib.NewReader(bytes.NewReader(rawStream)); err == nil {
			if decompressed, err := io.ReadAll(io.LimitReader(zr, 25*1024*1024)); err == nil {
				content = decompressed
			}
			_ = zr.Close()
		}
		extractPDFTextOps(content, &collected)
	}

	// Also scan top-level uncompressed content if no stream blocks yielded text
	if collected.Len() == 0 {
		extractPDFTextOps(data, &collected)
	}

	text := normalizeWhitespace(collected.String())
	if text == "" {
		return "", ErrExtractionFailed
	}
	return text, nil
}

func extractPDFTextOps(content []byte, out *strings.Builder) {
	ops := pdfTextOpRegex.FindAllSubmatch(content, -1)
	for _, op := range ops {
		if len(op[1]) > 0 {
			out.WriteString(unescapePDFString(string(op[1])))
			out.WriteByte(' ')
		} else if len(op[2]) > 0 {
			parts := pdfArrayStr.FindAllSubmatch(op[2], -1)
			for _, p := range parts {
				out.WriteString(unescapePDFString(string(p[1])))
			}
			out.WriteByte(' ')
		}
	}
}

func unescapePDFString(s string) string {
	r := strings.NewReplacer(
		`\(`, `(`,
		`\)`, `)`,
		`\\`, `\`,
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
	)
	return r.Replace(s)
}

func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	var clean []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return strings.Join(clean, "\n")
}
