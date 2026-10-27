package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// bookDocsMux serves Matthew's book page and the documents it lists. The book
// page is the German one, whose links lead to German addresses.
func bookDocsMux(t *testing.T) *http.ServeMux {
	mux := bibleMux(t)
	mux.HandleFunc("/en/wol/binav/r1/lp-e/nwtsty/40", wolFixture(t, "binav_de_40.html"))
	mux.HandleFunc("/de/wol/bibledocument/r10/lp-x/nwtsty/40/introduction", wolFixture(t, "intro_mt.html"))
	mux.HandleFunc("/de/wol/gallery/r10/lp-x/nwtsty/40", wolFixture(t, "gallery_mt.html"))
	mux.HandleFunc("/en/wol/bibledocument/r1/lp-e/nwtsty/40/overview", wolFixture(t, "overview_mt.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/1001070071", wolFixture(t, "book_table.html"))
	mux.HandleFunc("/wol/vidlink/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		wolFixture(t, "vidlink_gen.json")(w, r)
	})
	return mux
}

func TestBibleIntro(t *testing.T) {
	out, err := runCmd(t, bookDocsMux(t), "bible", "intro", "-l", "en", "-o", "raw", "40")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"## Introduction to Matthew",
		"- **Writer:** Matthew",
		"- **Time Covered:** 2 B.C.E.–33 C.E.",
		"### **Noteworthy Facts:**",
		"- Matthew had been a tax collector",
		"**Video:** [Introduction to Genesis](https://", // the fixture's video
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// the introduction lists all four facts: the table adds none
	if strings.Contains(out, "Writer(s)") {
		t.Errorf("table fact repeated:\n%s", out)
	}
}

func TestBibleOutlineOverview(t *testing.T) {
	out, err := runCmd(t, bookDocsMux(t), "bible", "outline", "-l", "en", "-o", "text", "40")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"Übersicht über Matthäus",
		"A. Genealogy of Jesus Christ (1:1-17)",
		"  Astrologers’ visit and Herod’s murderous plan (2:1-12)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// galleryVideoMux adds the gallery page of the gallery's one video, which
// names the address its player reads the renditions from, and those
// renditions. The mediator knows nothing of the video and fails the test when
// asked.
func galleryVideoMux(t *testing.T) *http.ServeMux {
	mux := bookDocsMux(t)
	mux.HandleFunc("/en/wol/gallery/r1/lp-e/nwtsty/40/1001072001", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div class="gallerySelectedItem">
<div class="mediaTitle"><h2><p>Video Introduction to the Book of Matthew</p></h2></div>
<div class="mediaContent video"><div class="videoContainer" data-json-src="/test/vidlink?docid=1001072001&amp;track=1"
  data-img-src="/en/wol/mp/r1/lp-e/nwtsty/2026/523"></div></div>
<div class="mediaCaption"><div class="captionWrapper"><div><p>What the book is about.</p></div></div></div>
</div>`)
	})
	mux.HandleFunc("/test/vidlink", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"files": {"E": {"MP4": [
			{"title": "Introduction to Matthew", "file": {"url": "http://%[1]s/files/mt_r240P.mp4"},
			 "label": "240p", "frameHeight": 240, "mimetype": "video/mp4", "filesize": 5, "duration": 391.3},
			{"title": "Introduction to Matthew", "file": {"url": "http://%[1]s/files/mt_r720P.mp4"},
			 "label": "720p", "frameHeight": 720, "mimetype": "video/mp4", "filesize": 9, "duration": 391.3}
		]}}}`, r.Host)
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("video"))
	})
	mux.HandleFunc("/apis/mediator/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a gallery video was looked up in the mediator: %s", r.URL)
		http.NotFound(w, r)
	})
	return mux
}

// A video of a book's gallery has no LANK: jw show and jw download reach it
// through its gallery page instead, and show numbers its files for download
// as it does for any other video.
func TestBibleGalleryVideo(t *testing.T) {
	mux := galleryVideoMux(t)
	cacheDir := t.TempDir()
	gallery := func() {
		t.Helper()
		if out, err := runCmdWithCache(t, mux, cacheDir, "bible", "gallery", "-l", "en", "40"); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}

	gallery()
	out, err := runCmdWithCache(t, mux, cacheDir, "show", "1", "-l", "en", "-o", "raw")
	if err != nil {
		t.Fatalf("show 1: %v\n%s", err, out)
	}
	for _, want := range []string{"# Video Introduction to the Book of Matthew", "What the book is about.",
		"6:31", "**240p**", "**720p**", "`jw download <n>`"} {
		if !strings.Contains(out, want) {
			t.Errorf("show 1: missing %q in\n%s", want, out)
		}
	}
	// no LANK to name, and none to look the words up by
	if strings.Contains(out, "LANK") || strings.Contains(out, "jw media text") {
		t.Errorf("show 1 offers a LANK it does not have:\n%s", out)
	}
	out, err = runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "en", "-d", t.TempDir())
	if err != nil || !strings.Contains(out, "mt_r240P.mp4") {
		t.Errorf("download 1 after show should be file 1 (240p): %v\n%s", err, out)
	}

	// straight from the gallery: the quality asked for, the best by default
	gallery()
	out, err = runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "en", "-d", t.TempDir())
	if err != nil || !strings.Contains(out, "mt_r720P.mp4") {
		t.Errorf("download 1: want the best rendition: %v\n%s", err, out)
	}
	out, err = runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "en", "-q", "240p", "-d", t.TempDir())
	if err != nil || !strings.Contains(out, "mt_r240P.mp4") {
		t.Errorf("download 1 -q 240p: %v\n%s", err, out)
	}
}

func TestBibleGallery(t *testing.T) {
	out, err := runCmd(t, bookDocsMux(t), "bible", "gallery", "-l", "en", "40")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"Media Gallery - Matthew", "Winter in Bethlehem — Matthew 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

// What the publications say about a chapter: each one under its name, the
// parts about the chapter with their items under their subheadings. A book
// named over several words still takes the chapter after it.
func TestBibleContent(t *testing.T) {
	mux := bibleMux(t)
	mux.HandleFunc("/en/wol/d/r1/lp-e/1101990062", wolFixture(t, "study_si_gen.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/2007722", wolFixture(t, "study_w_joel.html"))
	out, err := runCmd(t, mux, "bible", "content", "-l", "en", "-o", "text", "Genesis", "3")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"Content of Genesis 3",
		"Inspired – Genesis 3",
		"Sin and death enter the world; “seed” foretold as deliverer (3:1–5:5)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "preparation of the earth for human habitation") {
		t.Errorf("a part not about chapter 3:\n%s", out)
	}
	out, err = runCmd(t, mux, "bible", "content", "-l", "en", "-o", "text", "Joel", "2")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"Scriptural Questions Answered:", "Highlights – Joel 2", "What does it mean to ‘call on the name of Jehovah’?", "To call on God’s name means to know that name"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if _, err := runCmd(t, mux, "bible", "content", "-l", "en", "Joel", "9"); err == nil {
		t.Error("Joel has no chapter 9")
	}
}
