package cli

import (
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
