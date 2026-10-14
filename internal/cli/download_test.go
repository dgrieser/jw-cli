package cli

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// docSearchMux answers a jw.org search with what it finds besides media: a
// publication, a document, an issue of a periodical, a tract whose symbol ends
// in digits and a bible verse. pub-media has files for the first four; the
// mediator knows none of them and fails the test when asked.
func docSearchMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/tokens/jworg.jwt", func(w http.ResponseWriter, r *http.Request) {
		payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d}`, time.Now().Add(time.Hour).Unix()))
		fmt.Fprint(w, "h."+payload+".s")
	})
	mux.HandleFunc("/apis/search/results/E/all", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results": [
			{"type": "item", "subtype": "publication", "title": "Life and Ministry", "lank": "pub-lmd",
			 "links": {"jw.org": "https://www.jw.org/open?pub=lmd"}},
			{"type": "item", "subtype": "article", "title": "What Is the Kingdom of God?", "lank": "pa-2016330",
			 "links": {"jw.org": "https://www.jw.org/open?docid=2016330"}},
			{"type": "item", "subtype": "periodical", "title": "What Is God’s Kingdom?", "lank": "pi-wp_202005",
			 "links": {"jw.org": "https://www.jw.org/open?pub=wp20&issue=202005"}},
			{"type": "item", "subtype": "publication", "title": "What Is the Kingdom of God? (tract)", "lank": "pub-t-36",
			 "links": {"jw.org": "https://www.jw.org/open?pub=t-36"}},
			{"type": "item", "subtype": "verse", "title": "Daniel 2:44", "lank": "bv-27002044_nwtsty",
			 "links": {"jw.org": "https://www.jw.org/open?pub=nwtsty&bible=27002044"}}
		], "insight": {"total": {"value": 5}}}`)
	})
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var name string
		switch {
		case q.Get("pub") == "lmd" && q.Get("docid") == "":
			name = "lmd_E.pdf"
		case q.Get("docid") == "2016330" && q.Get("pub") == "":
			name = "wp_E_2016330.mp3"
		case q.Get("pub") == "wp" && q.Get("issue") == "202005":
			name = "wp_E_202005.pdf"
		case q.Get("pub") == "t-36" && q.Get("docid") == "":
			name = "t-36_E.pdf"
		default:
			http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		if formats := q.Get("fileformat"); formats != "" && !strings.Contains(formats, "PDF") && strings.HasSuffix(name, ".pdf") {
			http.Error(w, "no such format", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"pubName": "p", "files": {"E": {"PDF": [{
			"title": "t", "file": {"url": "http://%s/files/%s", "checksum": ""}, "filesize": 4, "mimetype": "application/pdf"
		}]}}}`, r.Host, name)
	})
	mux.HandleFunc("/apis/mediator/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a publication or a document was sent to the mediator: %s", r.URL)
		http.NotFound(w, r)
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("file"))
	})
	return mux
}

// A jw.org search finds publications and documents as well as media; each
// downloads what pub-media has for the key it names, never through the
// mediator, which knows media items only.
func TestDownloadSearchResultByKey(t *testing.T) {
	mux := docSearchMux(t)
	cacheDir := t.TempDir()
	if _, err := runCmdWithCache(t, mux, cacheDir, "search", "-l", "en", "kingdom"); err != nil {
		t.Fatal(err)
	}
	for index, want := range map[string]string{
		"1": "lmd_E.pdf",        // pub-lmd: the publication
		"2": "wp_E_2016330.mp3", // pa-2016330: the document, by its docid
		"3": "wp_E_202005.pdf",  // pi-wp_202005: the issue
		"4": "t-36_E.pdf",       // pub-t-36: the symbol, not a docid of 36
	} {
		out, err := runCmdWithCache(t, mux, cacheDir, "download", index, "-l", "en", "-d", t.TempDir())
		if err != nil {
			t.Errorf("download %s: %v\n%s", index, err, out)
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("download %s: want %s in\n%s", index, want, out)
		}
	}

	// -F narrows a publication down like it does for a symbol
	out, err := runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "en", "-F", "EPUB", "-d", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Life and Ministry") {
		t.Errorf("download 1 -F EPUB: want a not-found error naming the result, got %v\n%s", err, out)
	}

	// a bible verse has no files of its own
	_, err = runCmdWithCache(t, mux, cacheDir, "download", "5", "-l", "en", "-d", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "nothing downloadable") {
		t.Errorf("download 5: want nothing downloadable, got %v", err)
	}
}

// The keys a search result carries in -o json can be passed directly.
func TestDownloadByDocumentKey(t *testing.T) {
	out, err := runCmd(t, docSearchMux(t), "download", "pa-2016330", "-l", "en", "-d", t.TempDir())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "wp_E_2016330.mp3") {
		t.Errorf("want the document's file:\n%s", out)
	}
}
