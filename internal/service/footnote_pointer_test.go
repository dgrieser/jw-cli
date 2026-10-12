package service

import (
	"strings"
	"testing"
)

func TestPointerFootnote(t *testing.T) {
	path, label, ok := pointerFootnote(`<div class="groupFootnote"><p>Siehe <a href="https://wol.jw.org/de/wol/pc/r10/lp-x/1001070165/2/0">Worterklärungen</a>.</p></div>`)
	if !ok || path != "/de/wol/pc/r10/lp-x/1001070165/2/0" || label != "Worterklärungen" {
		t.Errorf("pointer = %q %q %v", path, label, ok)
	}
	for _, fn := range []string{
		`<p>Oder „Gerechtigkeit“.</p>`,
		`<p>Wörtl. „Same“. Siehe <a href="/de/wol/pc/r10/lp-x/1/2/0">Worterklärungen</a> und die Studienanmerkung.</p>`,
		`<p>Siehe <a href="/de/wol/bc/r10/lp-x/1/2/0">1Mo 1:1</a>.</p>`,
	} {
		if _, _, ok := pointerFootnote(fn); ok {
			t.Errorf("not a pointer, taken for one: %s", fn)
		}
	}
}

func TestWithoutHeader(t *testing.T) {
	got := withoutHeader(`<header><strong>Gerechtigkeit</strong></header><div class="bodyTxt"><p>Text</p></div>`)
	if strings.Contains(got, "header") || !strings.Contains(got, "<p>Text</p>") {
		t.Errorf("withoutHeader = %s", got)
	}
}

func TestVerseIDRefs(t *testing.T) {
	got := verseIDRefs([]int{61002005, 61002006, 61003001})
	if len(got) != 2 || got[0].Chapter != 2 || got[0].VerseStart != 5 || got[0].VerseEnd != 6 || got[1].Chapter != 3 {
		t.Errorf("verseIDRefs = %+v", got)
	}
}
