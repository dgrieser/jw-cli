package download

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

func TestRunBasic(t *testing.T) {
	content := []byte("hello jw world")
	sum := md5.Sum(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.Write(content)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	var lastWritten, lastTotal int64
	path, err := Run(context.Background(), httpx.New(), Job{
		URL: srv.URL + "/video_r720P.mp4", Dir: dir, Checksum: hex.EncodeToString(sum[:]),
	}, func(w, tot int64) { lastWritten, lastTotal = w, tot })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "video_r720P.mp4" {
		t.Errorf("filename = %s", filepath.Base(path))
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(content) {
		t.Errorf("content mismatch")
	}
	if lastWritten != int64(len(content)) || lastTotal != int64(len(content)) {
		t.Errorf("progress: written=%d total=%d", lastWritten, lastTotal)
	}
}

func TestRunChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	_, err := Run(context.Background(), httpx.New(), Job{
		URL: srv.URL + "/f.bin", Dir: dir, Checksum: "00000000000000000000000000000000",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum error, got %v", err)
	}
	// the corrupted partial must not survive to poison a resume
	if _, err := os.Stat(filepath.Join(dir, "f.bin.part")); !os.IsNotExist(err) {
		t.Errorf("corrupted .part file left on disk (stat err: %v)", err)
	}
}

func TestRunResume(t *testing.T) {
	full := []byte("0123456789abcdef")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rng := r.Header.Get("Range"); rng != "" {
			var start int64
			if _, err := parseRange(rng, &start); err != nil {
				t.Errorf("bad range %q", rng)
			}
			w.Header().Set("Content-Range", "bytes 8-15/16")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(full[start:])
			return
		}
		w.Write(full)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	// pre-existing partial file
	if err := os.WriteFile(filepath.Join(dir, "f.bin.part"), full[:8], 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := Run(context.Background(), httpx.New(), Job{
		URL: srv.URL + "/f.bin", Dir: dir, Filename: "f.bin",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(full) {
		t.Errorf("resumed content = %q, want %q", b, full)
	}
}

func parseRange(rng string, start *int64) (int, error) {
	return fscan(strings.TrimPrefix(rng, "bytes="), start)
}

func fscan(s string, start *int64) (int, error) {
	s = strings.TrimSuffix(s, "-")
	v, err := strconv.ParseInt(s, 10, 64)
	*start = v
	return 1, err
}

func TestPickVideo(t *testing.T) {
	files := []model.MediaFile{
		{Label: "240p", FrameHeight: 240, URL: "u240"},
		{Label: "720p", FrameHeight: 720, URL: "u720"},
		{Label: "480p", FrameHeight: 480, URL: "u480"},
	}
	cases := []struct {
		quality string
		want    string
	}{
		{"best", "u720"}, {"", "u720"}, {"worst", "u240"},
		{"720p", "u720"}, {"480", "u480"}, {"600p", "u480"}, {"100p", "u240"},
	}
	for _, c := range cases {
		got, err := PickVideo(files, c.quality)
		if err != nil {
			t.Errorf("PickVideo(%q): %v", c.quality, err)
			continue
		}
		if got.URL != c.want {
			t.Errorf("PickVideo(%q) = %s, want %s", c.quality, got.URL, c.want)
		}
	}
	if _, err := PickVideo(files, "bogus"); err == nil {
		t.Error("want error for invalid quality")
	}
	if _, err := PickVideo(nil, "best"); err == nil {
		t.Error("want error for empty file list")
	}
}

// TestFilenameExtensionFromContentType covers wol's extensionless media paths:
// an article illustration lives at /mp/r10/lp-x/w26/2026/296, so the extension
// has to come from the content type or the file lands as "296".
func TestFilenameExtensionFromContentType(t *testing.T) {
	for _, tc := range []struct {
		name        string
		path        string
		contentType string
		disposition string
		want        string
	}{
		{"extensionless image path", "/mp/r10/lp-x/w26/2026/296", "image/jpeg", "", "296.jpg"},
		{"png", "/mp/r10/lp-x/w26/2026/298", "image/png", "", "298.png"},
		{"charset parameter", "/mp/1", "image/jpeg; charset=binary", "", "1.jpg"},
		{"uppercase type", "/mp/2", "IMAGE/JPEG", "", "2.jpg"},
		// a name that already says what it is must not gain a second extension
		{"url extension wins", "/o/lffv_X_336_r240P.mp4", "video/mp4", "", "lffv_X_336_r240P.mp4"},
		{"mismatched type ignored", "/o/file.pdf", "image/jpeg", "", "file.pdf"},
		// nothing to conclude: leave the name alone rather than guess
		{"octet-stream", "/mp/3", "application/octet-stream", "", "3"},
		{"no content type", "/mp/4", "", "", "4"},
		{"unparseable type", "/mp/5", "not a type", "", "5"},
		// the server's own name is used, and completed the same way
		{"disposition", "/mp/6", "application/pdf", `attachment; filename="w_X_202405"`, "w_X_202405.pdf"},
		{"disposition with extension", "/mp/7", "image/jpeg", `attachment; filename="cover.png"`, "cover.png"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.contentType != "" {
				w.Header().Set("Content-Type", tc.contentType)
			}
			if tc.disposition != "" {
				w.Header().Set("Content-Disposition", tc.disposition)
			}
			w.Write([]byte("body"))
		}))
		dir := t.TempDir()
		got, err := Run(context.Background(), httpx.New(), Job{URL: srv.URL + tc.path, Dir: dir}, nil)
		srv.Close()
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if filepath.Base(got) != tc.want {
			t.Errorf("%s: filename = %q, want %q", tc.name, filepath.Base(got), tc.want)
		}
		// the .part file must not be left behind under either name
		for _, leftover := range []string{tc.want + ".part", filepath.Base(got) + ".part"} {
			if _, err := os.Stat(filepath.Join(dir, leftover)); err == nil {
				t.Errorf("%s: %s left behind", tc.name, leftover)
			}
		}
	}
}
