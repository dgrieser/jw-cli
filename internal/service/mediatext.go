package service

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/subtitles"
)

// A media item has words in two places. The recording of a publication's
// track — a song, an article read aloud — names in pub-media the document it
// sings or reads, and times each of its paragraphs: a song's lyrics are the
// song's page on jw.org, in every language, the songbook's own for the songs
// of the songbook. And a video comes with subtitles, a WebVTT file the
// mediator links, which is its transcript.

// trackKey reads a media item's key: pub-sjjm_1_VIDEO, pub-jwb_202401_1_VIDEO,
// pub-iam-4_18_AUDIO, docid-502014202_1_VIDEO.
var trackKey = regexp.MustCompile(`^(?:pub-([a-z0-9-]+?)(?:_([0-9]{6}|[0-9]{8}))?|docid-([0-9]+))_([0-9]+)_(?:VIDEO|AUDIO)$`)

// track is a publication's track, as a media item's key names it.
type track struct {
	Pub   string
	Issue string
	DocID int
	Track int
}

func parseTrack(lank string) (track, bool) {
	m := trackKey.FindStringSubmatch(lank)
	if m == nil {
		return track{}, false
	}
	t := track{Pub: m[1], Issue: m[2]}
	t.DocID, _ = strconv.Atoi(m[3])
	t.Track, _ = strconv.Atoi(m[4])
	return t, t.Track > 0 && (t.Pub != "" || t.DocID > 0)
}

// songbookPubs are the recordings numbered as the songbook's songs: the
// songs for the meetings, sung by a choir, played by instruments, sung by
// children. Their lyrics are the songbook's song of that number, whatever
// page the recording itself names.
var songbookPubs = map[string]bool{"sjjm": true, "sjjc": true, "sjji": true, "pksjj": true}

// songbookMeetings is the recording every song of the songbook has.
const songbookMeetings = "sjjm"

// markerSlack is how far a rendition may differ in length from the recording
// that times the lines and still be sung to the same times: a video of a
// song fades in a moment longer than the song.
const markerSlack = 1.5

// MediaText reads the words of a media item: the document its recording
// sings or reads, its subtitles, and — when it has no subtitles — the
// machine-made subtitles pub-media lists for it. What cannot be read is left
// out; only when nothing could be, and something failed, is that an error.
func (s *Service) MediaText(ctx context.Context, lang string, item model.MediaItem) (model.MediaText, error) {
	out := model.MediaText{LANK: item.LANK}
	for _, f := range item.Files {
		if f.SubtitlesURL != "" {
			out.SubtitlesURL = f.SubtitlesURL
			break
		}
	}
	var (
		pmErr, docErr, subErr, aiErr error
		aiCues                       []model.Cue
		wg                           sync.WaitGroup
	)
	if out.SubtitlesURL != "" {
		wg.Go(func() {
			var vtt string
			if vtt, subErr = s.HTTP.GetText(ctx, out.SubtitlesURL, nil); subErr == nil {
				out.Transcript = subtitles.ParseVTT(vtt)
			}
		})
	}
	if t, ok := parseTrack(item.LANK); ok {
		wg.Go(func() {
			pm, err := s.trackFiles(ctx, lang, t)
			if err != nil {
				// a track pub-media does not know has nothing more to say:
				// no fault of it
				if !errors.Is(err, pubmedia.ErrNotFound) {
					pmErr = err
				}
				return
			}
			var inner sync.WaitGroup
			inner.Go(func() {
				out.Document, docErr = s.mediaDocument(ctx, lang, item, t, pm)
			})
			if f, ok := pubFile(pm, lang, pubmedia.FormatAIVTT, t.Track); ok {
				inner.Go(func() {
					aiCues, aiErr = s.aiTranscript(ctx, lang, t, f)
				})
			}
			inner.Wait()
		})
	}
	wg.Wait()
	if len(out.Transcript) == 0 && len(aiCues) > 0 {
		// its link is signed for minutes: not one to hand on
		out.Transcript, out.AITranscript, out.SubtitlesURL = aiCues, true, ""
	}
	if out.Empty() {
		return out, errors.Join(pmErr, docErr, subErr, aiErr)
	}
	return out, nil
}

// mediaDocument is the document item's recording sings or reads, nil when it
// has none. pm is what pub-media lists for item's track t.
func (s *Service) mediaDocument(ctx context.Context, lang string, item model.MediaItem, t track, pm model.PubMedia) (*model.MediaDocument, error) {
	rec, _ := pubFile(pm, lang, "MP3", t.Track)
	var doc model.MediaDocument
	var err error
	if id := recordingDoc(rec); id > 0 {
		doc, err = s.JWOrg.Document(ctx, lang, id)
	}
	if songbookPubs[t.Pub] && !doc.Lyrics() && t.Pub != songbookMeetings {
		// the page of a song sung by children shows the video, the
		// songbook's page the lyrics
		song := track{Pub: songbookMeetings, Track: t.Track}
		if spm, serr := s.trackFiles(ctx, lang, song); serr == nil {
			if sung, ok := pubFile(spm, lang, "MP3", t.Track); ok && recordingDoc(sung) > 0 {
				if d, derr := s.JWOrg.Document(ctx, lang, recordingDoc(sung)); derr == nil && d.Lyrics() {
					doc, err, rec = d, nil, sung
				}
			}
		}
	}
	if len(doc.Blocks) == 0 {
		return nil, err
	}
	doc.Pub, doc.PubName = t.Pub, pm.PubName
	if rec.Markers != nil && rec.Markers.DocID == doc.DocID && plays(item, rec) {
		doc.Synced = timeLines(doc.Blocks, rec.Markers)
	}
	doc.Downloads = s.resolveDownloads(ctx, lang, doc.Downloads)
	return &doc, nil
}

// trackFiles is everything pub-media lists for a publication's track, in
// every format: the MP3 names the document it reads and times its lines,
// and the machine-made subtitles are listed only to a query that names no
// format.
func (s *Service) trackFiles(ctx context.Context, lang string, t track) (model.PubMedia, error) {
	return s.PubMedia.Links(ctx, pubmedia.Query{Pub: t.Pub, Issue: t.Issue, DocID: t.DocID, Track: t.Track, AnyFormat: true, Lang: lang})
}

// pubFile is the file of a format of track, or the format's only file when
// it is numbered no track.
func pubFile(pm model.PubMedia, lang, format string, track int) (model.PubFile, bool) {
	files := pm.Files[lang][format]
	for _, f := range files {
		if f.Track == track {
			return f, true
		}
	}
	if len(files) == 1 && files[0].Track == 0 {
		return files[0], true
	}
	return model.PubFile{}, false
}

// aiTranscript reads a track's machine-made subtitles. pub-media lists them
// before they are written — as an empty file — and signs their link for a
// few minutes only, so what is read is kept by the file's checksum rather
// than its link: an empty file is not asked for again until pub-media lists
// a changed one, which is when it may have been written. A link whose
// signature ran out while pub-media's answer was kept is asked for anew.
func (s *Service) aiTranscript(ctx context.Context, lang string, t track, f model.PubFile) ([]model.Cue, error) {
	vtt, err := s.signedText(ctx, f)
	if signatureExpired(err) && !httpx.Refreshing(ctx) {
		pm, perr := s.trackFiles(httpx.WithRefresh(ctx), lang, t)
		if perr != nil {
			return nil, errors.Join(err, perr)
		}
		nf, ok := pubFile(pm, lang, pubmedia.FormatAIVTT, t.Track)
		if !ok {
			return nil, nil
		}
		vtt, err = s.signedText(ctx, nf)
	}
	if err != nil {
		return nil, err
	}
	return subtitles.ParseVTT(vtt), nil
}

// maxSignedText bounds a machine-made subtitles file read: a long program's
// subtitles are a few hundred KiB.
const maxSignedText = 8 << 20

// signedText reads the file f, kept by its checksum: from the cache when a
// file of that checksum was read before, else past the response cache, which
// keeps by link, and a signed link is one only once.
func (s *Service) signedText(ctx context.Context, f model.PubFile) (string, error) {
	key := ""
	if f.Checksum != "" {
		u, err := url.Parse(f.URL)
		if err == nil {
			key = "aivtt/" + u.Host + u.Path + "@" + f.Checksum
		}
	}
	var text string
	if key != "" && s.Cache.Get(key, &text) {
		return text, nil
	}
	resp, err := s.HTTP.Get(ctx, f.URL, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSignedText))
	if err != nil {
		return "", err
	}
	if key != "" {
		s.Cache.Put(key, string(b))
	}
	return string(b), nil
}

// signatureExpired says a signed link was refused: its time ran out.
func signatureExpired(err error) bool {
	var se *httpx.StatusError
	return errors.As(err, &se) && (se.StatusCode == http.StatusForbidden || se.StatusCode == http.StatusUnauthorized)
}

func recordingDoc(f model.PubFile) int {
	if f.Markers != nil && f.Markers.DocID > 0 {
		return f.Markers.DocID
	}
	return f.DocID
}

// plays says item plays rec, or a rendition as long as it: the times of rec
// are the times of what the page plays.
func plays(item model.MediaItem, rec model.PubFile) bool {
	for _, f := range item.Files {
		if rec.Checksum != "" && f.Checksum == rec.Checksum {
			return true
		}
	}
	d := item.DurationSec
	if d == 0 && len(item.Files) > 0 {
		d = item.Files[len(item.Files)-1].Duration
	}
	return d > 0 && rec.Duration > 0 && math.Abs(d-rec.Duration) <= markerSlack
}

// timeLines gives each line the time the markers give its paragraph, and
// says whether any had one. A line the markers pass over has none.
func timeLines(blocks []model.TextBlock, m *model.Markers) bool {
	at := map[int]model.ParaMarker{}
	for _, p := range m.Paragraphs {
		at[p.PID] = p
	}
	timed := false
	for i := range blocks {
		for j := range blocks[i].Lines {
			l := &blocks[i].Lines[j]
			if p, ok := at[l.PID]; ok && l.PID > 0 {
				l.Start, l.End = p.Start, math.Round((p.Start+p.Duration)*1000)/1000
				timed = true
			}
		}
	}
	return timed
}

// resolveDownloads turns the pub-media queries a page links its downloads by
// into the files' own URLs. A download that cannot be resolved is left out.
func (s *Service) resolveDownloads(ctx context.Context, lang string, links []model.Link) []model.Link {
	var out []model.Link
	for _, l := range links {
		q, ok := pubMediaQuery(l.URL, lang)
		if !ok {
			continue
		}
		pm, err := s.PubMedia.Links(ctx, q)
		if err != nil {
			continue
		}
		for _, files := range pm.Files[lang] {
			if len(files) > 0 && files[0].URL != "" {
				out = append(out, model.Link{Label: l.Label, URL: files[0].URL})
				break
			}
		}
	}
	return out
}

// pubMediaQuery reads a GETPUBMEDIALINKS link of a page as a query.
func pubMediaQuery(raw, lang string) (pubmedia.Query, bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.Contains(u.Path, "GETPUBMEDIALINKS") {
		return pubmedia.Query{}, false
	}
	v := u.Query()
	q := pubmedia.Query{Pub: v.Get("pub"), Issue: v.Get("issue"), Lang: lang}
	q.DocID, _ = strconv.Atoi(v.Get("docid"))
	q.Track, _ = strconv.Atoi(v.Get("track"))
	if f := v.Get("fileformat"); f != "" {
		q.Formats = strings.Split(f, ",")
	}
	return q, q.Pub != "" || q.DocID != 0
}
