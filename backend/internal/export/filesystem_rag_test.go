package export

import (
	"strings"
	"testing"
)

func TestNormalizePDFExtractedText_ReflowsFragmentedWords(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, 120)
	for i := 0; i < 10; i++ {
		lines = append(lines,
			"Entwurfsverfasser:",
			"Notariat",
			"Borsum",
			"&",
			"Dr.",
			"Bortfeld",
			"Sedanstraße 4",
			"30161",
			"Hannover",
			"",
			"Genehmigungserklärung",
			"Hiermit",
			"genehmige",
			"ich",
			"",
		)
	}
	raw := strings.Join(lines, "\n")

	normalized := normalizePDFExtractedText(raw)

	if strings.Contains(normalized, "Notariat\nBorsum\n&") {
		t.Fatalf("expected fragmented lines to be reflowed, got: %q", normalized)
	}
	if !strings.Contains(normalized, "Entwurfsverfasser: Notariat Borsum & Dr. Bortfeld") {
		t.Fatalf("expected joined sentence, got: %q", normalized)
	}
}

func TestLooksFragmentedPDFText(t *testing.T) {
	t.Parallel()

	builder := strings.Builder{}
	for i := 0; i < 60; i++ {
		builder.WriteString("word\n")
	}

	if !looksFragmentedPDFText(builder.String()) {
		t.Fatalf("expected fragmented text to be detected")
	}
}

func TestCleanLegacyBinaryExtractedText_RemovesViewStateAndHTMLNoise(t *testing.T) {
	t.Parallel()

	raw := `<html><body><div>Law no (22) of 2004</div><div>Valid article text.</div>` +
		`<input type="hidden" name="__VIEWSTATE" value="` + strings.Repeat("A", 260) + `"/>` +
		`<script>window.__foo = "bar";</script></body></html>`

	cleaned := cleanLegacyBinaryExtractedText(raw)

	if !strings.Contains(cleaned, "Law no (22) of 2004") {
		t.Fatalf("expected useful text to remain, got: %q", cleaned)
	}
	if !strings.Contains(cleaned, "Valid article text.") {
		t.Fatalf("expected content text to remain, got: %q", cleaned)
	}
	if strings.Contains(strings.ToLower(cleaned), "__viewstate") {
		t.Fatalf("expected __VIEWSTATE payload to be removed, got: %q", cleaned)
	}
	if strings.Contains(cleaned, "<input") || strings.Contains(cleaned, "<html") {
		t.Fatalf("expected html tags to be removed, got: %q", cleaned)
	}
}

func TestStripBinaryNoiseLines_DropsLongEncodedPayload(t *testing.T) {
	t.Parallel()

	line := "normal text\n" + strings.Repeat("QWERTY1234567890+/", 16) + "==\nkeep this line"
	cleaned := stripBinaryNoiseLines(line)

	if strings.Contains(cleaned, "QWERTY1234567890+/") {
		t.Fatalf("expected encoded payload line to be removed, got: %q", cleaned)
	}
	if !strings.Contains(cleaned, "normal text") || !strings.Contains(cleaned, "keep this line") {
		t.Fatalf("expected non-noise lines to be preserved, got: %q", cleaned)
	}
}
