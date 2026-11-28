package rag

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installFakePDFTools(t *testing.T) {
	t.Helper()
	binDir := t.TempDir()
	scripts := map[string]string{
		"pdfinfo": `#!/bin/sh
[ "$PDFINFO_FAIL" = "1" ] && exit 1
[ "$PDFINFO_SLEEP" = "1" ] && sleep 1
printf 'Pages: %s\n' "${PDF_PAGES:-1}"
`,
		"pdftotext": `#!/bin/sh
[ "$PDFTOTEXT_FAIL" = "1" ] && exit 1
if [ "$PDF_TEXT_LARGE" = "1" ]; then
  printf '%*s' 10485761 ''
else
  printf '%s' "$PDF_TEXT"
fi
`,
		"pdftoppm": `#!/bin/sh
[ "$PDFTOPPM_FAIL" = "1" ] && exit 1
shift 9
prefix=$2
i=1
while [ "$i" -le "${PDF_PAGES:-1}" ]; do
  : > "$prefix-$i.jpg"
  i=$((i + 1))
done
`,
		"tesseract": `#!/bin/sh
[ "$TESSERACT_FAIL" = "1" ] && exit 1
printf '%s' "$OCR_TEXT"
`,
	}
	for name, content := range scripts {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PDF_PAGES", "1")
	t.Setenv("PDF_TEXT", "")
	t.Setenv("OCR_TEXT", "")
}

func TestExtractPDFUsesUTF8TextExtraction(t *testing.T) {
	installFakePDFTools(t)
	t.Setenv("PDF_TEXT", "Compressed-stream text: 你好世界")

	text, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\ncompressed stream"))
	if err != nil {
		t.Fatalf("ExtractText returned an error: %v", err)
	}
	if text != "Compressed-stream text: 你好世界" {
		t.Fatalf("unexpected extracted text: %q", text)
	}
}

func TestExtractPDFPopplerExtractsCompressedUnicode(t *testing.T) {
	for _, tool := range []string{"pdfinfo", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is unavailable; run this integration test in the server image", tool)
		}
	}

	text, err := ExtractText(DocTypePDF, compressedUnicodePDF(t))
	if err != nil {
		t.Fatalf("ExtractText returned an error: %v", err)
	}
	if !strings.Contains(text, "Hello 你好世界!") {
		t.Fatalf("compressed Unicode text was not extracted: %q", text)
	}
}

func compressedUnicodePDF(t *testing.T) []byte {
	t.Helper()
	compress := func(data []byte) []byte {
		var output bytes.Buffer
		writer := zlib.NewWriter(&output)
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	streamObject := func(data []byte) []byte {
		header := []byte(fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n", len(data)))
		return append(append(header, data...), []byte("\nendstream")...)
	}

	content := compress([]byte("BT /F1 12 Tf 72 200 Td <0001000200030003000400050006000700080009000A> Tj ET"))
	cmap := compress([]byte(`/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
/CMapName /Adobe-Identity-UCS def
/CMapType 2 def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
10 beginbfchar
<0001> <0048>
<0002> <0065>
<0003> <006C>
<0004> <006F>
<0005> <0020>
<0006> <4F60>
<0007> <597D>
<0008> <4E16>
<0009> <754C>
<000A> <0021>
endbfchar
endcmap
CMapName currentdict /CMap defineresource pop
end
end`))
	objects := [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"),
		streamObject(content),
		[]byte("<< /Type /Font /Subtype /Type0 /BaseFont /AegisTest /Encoding /Identity-H /DescendantFonts [6 0 R] /ToUnicode 8 0 R >>"),
		[]byte("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /AegisTest /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 7 0 R /CIDToGIDMap /Identity >>"),
		[]byte("<< /Type /FontDescriptor /FontName /AegisTest /Flags 4 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 1000 /Descent 0 /CapHeight 700 /StemV 80 >>"),
		streamObject(cmap),
	}

	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n", i+1)
		pdf.Write(object)
		pdf.WriteString("\nendobj\n")
	}
	xrefOffset := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)
	return pdf.Bytes()
}

func TestExtractPDFUsesOCRForScannedPages(t *testing.T) {
	installFakePDFTools(t)
	t.Setenv("PDF_PAGES", "2")
	t.Setenv("OCR_TEXT", "识别出的扫描文本")

	text, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\nscanned"))
	if err != nil {
		t.Fatalf("ExtractText returned an error: %v", err)
	}
	if text != "识别出的扫描文本\n识别出的扫描文本" {
		t.Fatalf("unexpected OCR text: %q", text)
	}
}

func TestExtractPDFUsesOCRForLongScannedPDF(t *testing.T) {
	installFakePDFTools(t)
	t.Setenv("PDF_PAGES", "88")
	t.Setenv("PDF_TEXT", "Only a few characters from the scanned pages")
	t.Setenv("OCR_TEXT", "识别出的扫描文本")

	text, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\nscanned book"))
	if err != nil {
		t.Fatalf("ExtractText returned an error: %v", err)
	}
	if count := strings.Count(text, "识别出的扫描文本"); count != 88 {
		t.Fatalf("expected OCR text from all 88 pages, got %d pages", count)
	}
}

func TestExtractPDFRejectsEmptyOutputAndToolFailures(t *testing.T) {
	t.Run("empty text and OCR", func(t *testing.T) {
		installFakePDFTools(t)
		_, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\nempty"))
		if !errors.Is(err, ErrExtractionFailed) || !strings.Contains(err.Error(), "no readable text") {
			t.Fatalf("expected actionable empty extraction error, got %v", err)
		}
	})

	t.Run("Poppler failure", func(t *testing.T) {
		installFakePDFTools(t)
		t.Setenv("PDFINFO_FAIL", "1")
		_, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\ninvalid"))
		if !errors.Is(err, ErrExtractionFailed) || !strings.Contains(err.Error(), "PDF inspection failed") {
			t.Fatalf("expected sanitized Poppler error, got %v", err)
		}
	})

	t.Run("OCR failure", func(t *testing.T) {
		installFakePDFTools(t)
		t.Setenv("TESSERACT_FAIL", "1")
		_, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\nscanned"))
		if !errors.Is(err, ErrExtractionFailed) || !strings.Contains(err.Error(), "OCR failed") {
			t.Fatalf("expected sanitized OCR error, got %v", err)
		}
	})
}

func TestExtractPDFEnforcesProcessingLimits(t *testing.T) {
	t.Run("page count", func(t *testing.T) {
		installFakePDFTools(t)
		t.Setenv("PDF_PAGES", "101")
		_, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\nmany pages"))
		if !errors.Is(err, ErrExtractionFailed) || !strings.Contains(err.Error(), "100-page processing limit") {
			t.Fatalf("expected page-limit error, got %v", err)
		}
	})

	t.Run("text output size", func(t *testing.T) {
		installFakePDFTools(t)
		t.Setenv("PDF_TEXT_LARGE", "1")
		_, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\nlarge text"))
		if !errors.Is(err, ErrExtractionFailed) || !strings.Contains(err.Error(), "exceeds the processing limit") {
			t.Fatalf("expected output-limit error, got %v", err)
		}
	})

	t.Run("source size", func(t *testing.T) {
		data := make([]byte, DefaultMaxUploadBytes+1)
		_, err := ExtractText(DocTypePDF, data)
		if !errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("expected payload limit error, got %v", err)
		}
	})

	t.Run("command timeout", func(t *testing.T) {
		installFakePDFTools(t)
		t.Setenv("PDFINFO_SLEEP", "1")
		oldTimeout := pdfCommandTimeout
		pdfCommandTimeout = 25 * time.Millisecond
		t.Cleanup(func() { pdfCommandTimeout = oldTimeout })
		_, err := ExtractText(DocTypePDF, []byte("%PDF-1.7\ntimeout"))
		if !errors.Is(err, ErrExtractionFailed) || !strings.Contains(err.Error(), "PDF inspection failed") {
			t.Fatalf("expected sanitized timeout error, got %v", err)
		}
	})
}
