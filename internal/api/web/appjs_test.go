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

// An empty list sent to PUT /filters or PUT /video-rules replaces the
// stored settings with the defaults. The Sections page refuses to send one
// (its rows come from Stash, so none means Stash was unreachable); the
// rules table says so and shows the defaults it now holds.
func TestAppJS_EmptySavesNeverWipeSilently(t *testing.T) {
	js := appJS(t)
	for _, want := range []string{
		"if (!list.length) { setMsg($('#filters-msg'), 'Nothing to save: reload this page once Stash is reachable.', 'warn'); return; }",
		"await api('PUT', '/filters', list)",
		"const showDefaultRules = () => {",
		"$('#default-rules').content.querySelectorAll('[data-rule]')",
		"await api('PUT', '/video-rules', list);\n        if (!list.length) {\n          showDefaultRules();\n          setMsg($('#rules-msg'), 'Defaults restored: the table was empty, so the default rules were saved instead.', 'ok');",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
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
