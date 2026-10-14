package render

import (
	"strings"
	"testing"
)

// What a wol document page carries inside its article besides the document,
// as captured from /en/wol/d/r1/lp-e/1102025912 (shortened): the image
// viewer's lightbox, and the hidden fields the page's scripts read, two of
// them URLs. A study article's answer field belongs to the document.
const documentChrome = `<article id="article">
<p id="p15" data-pid="15"><strong>In what ways did Caleb show courage?</strong></p>
<div class="gen-field" id="p16" data-pid="16">
<label for="tt12" id="tt10" class="dc-screenReaderText">Your answer</label>
<textarea id="tt12" name="textarea-1"></textarea>
</div>
<p id="p17" data-pid="17" data-img-src="/en/wol/mp/r1/lp-e/wcg/2025/297">Caleb <a href="/en/wol/bc/r1/lp-e/1102025912/3/0" data-bid="4-1" class="b">Num. 14:8</a></p>
<div class="pswp" tabindex="-1" role="dialog" aria-hidden="true">
    <div class="pswp__bg"></div>
    <div class="pswp__scroll-wrap">
        <!-- Container that holds slides. -->
        <div class="pswp__container"><div class="pswp__item"></div></div>
        <div class="pswp__ui pswp__ui--hidden">
            <div class="pswp__top-bar"><div class="pswp__counter"></div>
                <button class="pswp__button pswp__button--close" title="Close (Esc)"></button>
                <button class="pswp__button pswp__button--zoom" title="Zoom in/out"></button>
            </div>
            <div class="pswp__caption"><div class="pswp__caption__center"></div></div>
        </div>
    </div>
</div>
<input type="hidden" id="pubMediaServer" value="https://b.jw-cdn.org/apis/pub-media/GETPUBMEDIALINKS"/>
<input type="hidden" id="docId" value="1102025912"/>
<input type="hidden" id="shareBaseUrl" value="https://www.jw.org/finder?wtlocale=E&docid=1102025912&srctype=wol&srcid=share"/>
<input type="hidden" id="contentThumbnail" value="/en/wol/d/r1/lp-e/1102025912/thumbnail"/>
</article>`

func TestRenderDropsPageChrome(t *testing.T) {
	got, err := Render(documentChrome, HTML, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"pswp", "<input", "<button", "GETPUBMEDIALINKS", "shareBaseUrl", "Close (Esc)"} {
		if strings.Contains(got, gone) {
			t.Errorf("html keeps %q:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"In what ways did Caleb show courage?", "<textarea", "Your answer", "Num. 14:8"} {
		if !strings.Contains(got, kept) {
			t.Errorf("html lost %q:\n%s", kept, got)
		}
	}
}

// No format prints a comment: a jw.org article ends in one.
func TestRenderDropsComments(t *testing.T) {
	const fragment = `<div class="bodyTxt"><p>Revelation 21:1-4.</p><!--THE END--></div>`
	for _, f := range []Format{Markdown, Raw, HTML, Text} {
		got, err := Render(fragment, f, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "THE END") || strings.Contains(got, "<!--") {
			t.Errorf("%v keeps the comment: %q", f, got)
		}
	}
}

// wol's study questions, as captured from /en/wol/d/r1/lp-e/1102025912: an
// ordered list whose every item prints its own number.
const studyQuestions = `<ol class="du-listStyleType--source"><li><p id="p18" data-pid="18"><span class="txtSrcBullet"><strong>1.</strong> </span><strong>Why can we say that</strong> <a href="/en/wol/bc/r1/lp-e/1102025912/2/0" data-bid="3-1" class="b"><strong>Numbers 13:23</strong></a> <strong>does not exaggerate the produce of the Promised Land?</strong></p>
</li>
<li><p id="p20" data-pid="20"><span class="txtSrcBullet"><strong>2.</strong> </span><strong>Was the Promised Land really abundant with “milk and honey”?</strong></p>
</li></ol>
<ol><li>a list numbered by the list</li><li>as lists are</li></ol>`

// An item that carries its own number is numbered once, with the number it
// carries; a plain ordered list stays one.
func TestRenderSelfNumberedList(t *testing.T) {
	md, err := Render(studyQuestions, Raw, Options{NoURLs: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "1. **1.**") || strings.Contains(md, "2. **2.**") {
		t.Errorf("numbered twice:\n%s", md)
	}
	for _, want := range []string{"**1.** **Why can we say that**", "**2.** **Was the Promised Land", "1. a list numbered by the list", "2. as lists are"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown misses %q:\n%s", want, md)
		}
	}
	txt, err := Render(studyQuestions, Text, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(txt, "- 1.") || !strings.Contains(txt, "1. Why can we say that") {
		t.Errorf("text:\n%s", txt)
	}
}

// Under --no-urls the html output names no address at all: not the link
// targets, not the hidden fields, not a source kept in a data attribute.
func TestRenderNoURLsLeavesNoAddress(t *testing.T) {
	got, err := Render(documentChrome, HTML, Options{BaseURL: "https://wol.jw.org", NoURLs: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"://", `="/`, "jw.org", "jw-cdn"} {
		if strings.Contains(got, url) {
			t.Errorf("--no-urls html keeps %q:\n%s", url, got)
		}
	}
	if !strings.Contains(got, "Num. 14:8") || !strings.Contains(got, `data-pid="17"`) {
		t.Errorf("--no-urls lost the text or its other attributes:\n%s", got)
	}
}
