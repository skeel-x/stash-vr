package web

import (
	"io/fs"
	"strings"
	"testing"

	"stash-vr/internal/static"
)

// appJS is the page script as served; the pages' behaviour lives there,
// so these tests pin what it must do.
func appJS(t *testing.T) string {
	t.Helper()
	js, err := fs.ReadFile(static.Fs, "app.js")
	if err != nil {
		t.Fatal(err)
	}
	return string(js)
}

func TestAppJS_CopyFallsBackToExecCommandOffSecureOrigins(t *testing.T) {
	js := appJS(t)
	start := strings.Index(js, "async function copyText")
	if start < 0 {
		t.Fatal("expected a copyText helper")
	}
	body := js[start:]
	body = body[:strings.Index(body, "document.querySelectorAll('[data-copy]')")]
	for _, want := range []string{
		// The Clipboard API is only tried where it exists, on a secure origin.
		"navigator.clipboard && window.isSecureContext",
		// Plain http gets the textarea fallback.
		"document.createElement('textarea')",
		"document.execCommand('copy')",
		"ta.remove()",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("copyText missing %q", want)
		}
	}
	// "Copied" is shown only when one of the two ways reported success.
	if !strings.Contains(js, "(await copyText(btn.dataset.copy)) ? 'Copied' : 'Select the address'") {
		t.Fatal("the copy button must show Copied only on success")
	}
}
