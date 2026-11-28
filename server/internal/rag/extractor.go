package rag

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrExtractionFailed = errors.New("failed to extract text from document")

const (
	maxPDFPages        = 100
	maxPDFOutputBytes  = 10 * 1024 * 1024
	minPDFRunesPerPage = 20
	pdfRenderScale     = 1800
)

var (
	pdfCommandTimeout = 10 * time.Minute
	pdfPagesRegex     = regexp.MustCompile(`(?m)^Pages:\s*(\d+)\s*$`)
	errPDFOutputLimit = errors.New("PDF tool output exceeds configured limit")
)

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

func extractPDF(data []byte) (string, error) {
	if int64(len(data)) > DefaultMaxUploadBytes {
		return "", ErrPayloadTooLarge
	}

	dir, err := os.MkdirTemp("", "aegis-rag-pdf-*")
	if err != nil {
		return "", fmt.Errorf("%w: unable to prepare temporary PDF processing", ErrExtractionFailed)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	inputPath := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return "", fmt.Errorf("%w: unable to prepare temporary PDF processing", ErrExtractionFailed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pdfCommandTimeout)
	defer cancel()

	info, err := runPDFCommand(ctx, "pdfinfo", []string{inputPath}, 128*1024)
	if err != nil {
		return "", fmt.Errorf("%w: PDF inspection failed; ensure Poppler is installed and the file is readable", ErrExtractionFailed)
	}
	pageMatch := pdfPagesRegex.FindSubmatch(info)
	if len(pageMatch) != 2 {
		return "", fmt.Errorf("%w: PDF page count could not be determined", ErrExtractionFailed)
	}
	pages, err := strconv.Atoi(string(pageMatch[1]))
	if err != nil || pages < 1 {
		return "", fmt.Errorf("%w: PDF contains no readable pages", ErrExtractionFailed)
	}
	if pages > maxPDFPages {
		return "", fmt.Errorf("%w: PDF exceeds the %d-page processing limit", ErrExtractionFailed, maxPDFPages)
	}

	textBytes, err := runPDFCommand(ctx, "pdftotext", []string{"-enc", "UTF-8", "-nopgbrk", inputPath, "-"}, maxPDFOutputBytes)
	if err != nil && errors.Is(err, errPDFOutputLimit) {
		return "", fmt.Errorf("%w: extracted PDF text exceeds the processing limit", ErrExtractionFailed)
	}
	if err == nil && hasSufficientPDFText(textBytes, pages) {
		return normalizeWhitespace(string(textBytes)), nil
	}

	text, err := ocrPDF(ctx, dir, inputPath, pages)
	if err != nil {
		if errors.Is(err, errPDFOutputLimit) {
			return "", fmt.Errorf("%w: OCR text exceeds the processing limit", ErrExtractionFailed)
		}
		return "", fmt.Errorf("%w: scanned PDF OCR failed; ensure Poppler and English/Chinese Tesseract data are installed", ErrExtractionFailed)
	}
	if !usablePDFText([]byte(text)) {
		return "", fmt.Errorf("%w: no readable text found; upload a clearer PDF or an OCR-ready scan", ErrExtractionFailed)
	}
	return normalizeWhitespace(text), nil
}

func ocrPDF(ctx context.Context, dir, inputPath string, pages int) (string, error) {
	prefix := filepath.Join(dir, "page")
	_, err := runPDFCommand(ctx, "pdftoppm", []string{
		"-f", "1",
		"-l", strconv.Itoa(pages),
		"-scale-to", strconv.Itoa(pdfRenderScale),
		"-jpeg",
		"-jpegopt", "quality=85",
		inputPath,
		prefix,
	}, 1024)
	if err != nil {
		return "", err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var imagePaths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "page-") && strings.EqualFold(filepath.Ext(entry.Name()), ".jpg") {
			imagePaths = append(imagePaths, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Slice(imagePaths, func(i, j int) bool {
		pageNumber := func(path string) int {
			name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "page-"), filepath.Ext(path))
			number, _ := strconv.Atoi(name)
			return number
		}
		return pageNumber(imagePaths[i]) < pageNumber(imagePaths[j])
	})
	if len(imagePaths) == 0 || len(imagePaths) > pages {
		return "", errors.New("rendered page count is invalid")
	}

	var text strings.Builder
	for _, imagePath := range imagePaths {
		remaining := maxPDFOutputBytes - text.Len() - 1
		if remaining <= 0 {
			return "", errPDFOutputLimit
		}
		pageText, err := runPDFCommand(ctx, "tesseract", []string{
			imagePath, "stdout", "-l", "eng+chi_sim+chi_tra", "--psm", "3",
		}, remaining)
		if err != nil {
			return "", err
		}
		text.Write(pageText)
		text.WriteByte('\n')
	}
	return text.String(), nil
}

type limitedPDFOutput struct {
	data     []byte
	limit    int
	exceeded bool
	onExceed func()
}

func (b *limitedPDFOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-len(b.data) {
		b.exceeded = true
		b.onExceed()
		return len(p), nil
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func runPDFCommand(ctx context.Context, name string, args []string, outputLimit int) ([]byte, error) {
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, name, args...)
	output := &limitedPDFOutput{limit: outputLimit, onExceed: cancel}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if output.exceeded {
		return nil, errPDFOutputLimit
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s failed", name)
	}
	return output.data, nil
}

func usablePDFText(data []byte) bool {
	return utf8.Valid(data) && !bytes.ContainsRune(data, '\x00') && strings.TrimSpace(string(data)) != ""
}

func hasSufficientPDFText(data []byte, pages int) bool {
	return usablePDFText(data) && (pages == 1 || utf8.RuneCount(data) >= pages*minPDFRunesPerPage)
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
