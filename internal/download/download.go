// Package download fetches media files to disk: atomic writes via a .part
// temp file, HTTP Range resume, optional MD5 checksum verification.
package download

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// Job describes one file to download.
type Job struct {
	URL      string
	Dir      string // destination directory ("" = cwd)
	Filename string // optional explicit name; default derived from response/URL
	Checksum string // optional MD5 to verify
	Size     int64  // expected size when known (for progress)
}

// Progress is called as bytes arrive.
type Progress func(written, total int64)

// Run downloads the job and returns the final file path.
func Run(ctx context.Context, hc *httpx.Client, j Job, p Progress) (string, error) {
	dir := j.Dir
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.URL, nil)
	if err != nil {
		return "", err
	}

	// resume a partial download if the name is already known
	var partial int64
	var partPath string
	if j.Filename != "" {
		partPath = filepath.Join(dir, j.Filename+".part")
		if st, err := os.Stat(partPath); err == nil && st.Size() > 0 {
			partial = st.Size()
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", partial))
		}
	}

	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPartialContent && partial > 0:
		// resuming
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		partial = 0 // server ignored the range (or fresh download)
	default:
		return "", &httpx.StatusError{URL: j.URL, StatusCode: resp.StatusCode}
	}

	name := j.Filename
	if name == "" {
		name = filenameFor(resp, j.URL)
		partPath = filepath.Join(dir, name+".part")
		partial = 0
	}
	final := filepath.Join(dir, name)

	flags := os.O_CREATE | os.O_WRONLY
	if partial > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return "", err
	}

	total := j.Size
	if total == 0 && resp.ContentLength > 0 {
		total = partial + resp.ContentLength
	}
	written := partial
	buf := make([]byte, 128*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return "", werr
			}
			written += int64(n)
			if p != nil {
				p(written, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return "", fmt.Errorf("download %s: %w", j.URL, rerr)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	if j.Checksum != "" {
		if err := verifyMD5(partPath, j.Checksum); err != nil {
			// drop the corrupted partial so the next attempt starts clean
			// instead of resuming from bad bytes
			_ = os.Remove(partPath)
			return "", err
		}
	}
	if err := os.Rename(partPath, final); err != nil {
		return "", err
	}
	return final, nil
}

func verifyMD5(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filepath.Base(path), got, want)
	}
	return nil
}

var unsafeName = regexp.MustCompile(`[^\w.\- ()\[\]]+`)

// filenameFor derives a safe filename from Content-Disposition or the URL.
func filenameFor(resp *http.Response, rawURL string) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn := params["filename"]; fn != "" {
				return sanitizeName(fn)
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		if base := path.Base(u.Path); base != "." && base != "/" {
			return sanitizeName(base)
		}
	}
	return "download"
}

func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = unsafeName.ReplaceAllString(s, "_")
	s = strings.Trim(s, " .")
	if s == "" {
		return "download"
	}
	return s
}

// PickVideo selects a rendition by quality spec: "best", "worst", or a label
// like "720p" / bare height like "720" (falls back to the closest lower
// quality when the exact one is missing).
func PickVideo(files []model.MediaFile, quality string) (model.MediaFile, error) {
	if len(files) == 0 {
		return model.MediaFile{}, fmt.Errorf("no downloadable files")
	}
	sorted := make([]model.MediaFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return height(sorted[i]) < height(sorted[j]) })

	switch strings.ToLower(quality) {
	case "", "best":
		return sorted[len(sorted)-1], nil
	case "worst":
		return sorted[0], nil
	}
	want, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(quality), "p"))
	if err != nil {
		return model.MediaFile{}, fmt.Errorf("invalid quality %q (want best, worst, or e.g. 720p)", quality)
	}
	best := sorted[0]
	found := false
	for _, f := range sorted {
		if height(f) <= want {
			best = f
			found = true
		}
	}
	if !found {
		best = sorted[0] // everything is higher than requested; take lowest
	}
	return best, nil
}

func height(f model.MediaFile) int {
	if f.FrameHeight > 0 {
		return f.FrameHeight
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(f.Label), "p")); err == nil {
		return n
	}
	return 0
}
