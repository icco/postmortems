package postmortems

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateFile_EndBeforeStart asserts ValidateFile rejects a
// postmortem whose end_time is before its start_time.
func TestValidateFile_EndBeforeStart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := `---
uuid: "11111111-1111-1111-1111-111111111111"
url: "https://example.com/incident"
start_time: 2024-03-20T00:00:00Z
end_time: 2024-03-15T00:00:00Z
company: "Example"
product: ""

---

Description.
`
	fp := filepath.Join(dir, "11111111-1111-1111-1111-111111111111.md")
	if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := ValidateFile(fp); err == nil {
		t.Fatalf("ValidateFile: want error, got nil")
	}
}

// TestValidateFile_EmptyDatesAllowed asserts that empty (zero) start
// and end times do not fail validation.
func TestValidateFile_EmptyDatesAllowed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	body := `---
uuid: "22222222-2222-2222-2222-222222222222"
url: "https://example.com/incident"
company: "Example"
product: ""

---

Description.
`
	fp := filepath.Join(dir, "22222222-2222-2222-2222-222222222222.md")
	if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := ValidateFile(fp); err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
}
