package service

import (
	"context"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
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
// sings or reads, and its subtitles. What cannot be read is left out; only
// when nothing could be, and something failed, is that an error.
func (s *Service) MediaText(ctx context.Context, lang string, item model.MediaItem) (model.MediaText, error) {
	out := model.MediaText{LANK: item.LANK}
	var docErr, subErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		out.Document, docErr = s.mediaDocument(ctx, lang, item)
	})
	for _, f := range item.Files {
		if f.SubtitlesURL != "" {
			out.SubtitlesURL = f.SubtitlesURL
			break
		}
	}
	if out.SubtitlesURL != "" {
		wg.Go(func() {
			var vtt string
			if vtt, subErr = s.HTTP.GetText(ctx, out.SubtitlesURL, nil); subErr == nil {
				out.Transcript = subtitles.ParseVTT(vtt)
			}
		})
	}
	wg.Wait()
	if out.Empty() {
		return out, errors.Join(docErr, subErr)
	}
	return out, nil
}

// mediaDocument is the document item's recording sings or reads, nil when it
// has none.
func (s *Service) mediaDocument(ctx context.Context, lang string, item model.MediaItem) (*model.MediaDocument, error) {
	t, ok := parseTrack(item.LANK)
	if !ok || t.Pub == "" {
		return nil, nil
	}
	rec, pubName, err := s.recording(ctx, lang, t)
	if errors.Is(err, pubmedia.ErrNotFound) {
		// a track pub-media does not know has no document: no fault of it
		err = nil
	} else if err != nil {
		return nil, err
	}
	var doc model.MediaDocument
	if id := recordingDoc(rec); id > 0 {
		doc, err = s.JWOrg.Document(ctx, lang, id)
	}
	if songbookPubs[t.Pub] && !doc.Lyrics() && t.Pub != songbookMeetings {
		// the page of a song sung by children shows the video, the
		// songbook's page the lyrics
		song := track{Pub: songbookMeetings, Track: t.Track}
		if sung, _, serr := s.recording(ctx, lang, song); serr == nil && recordingDoc(sung) > 0 {
			if d, derr := s.JWOrg.Document(ctx, lang, recordingDoc(sung)); derr == nil && d.Lyrics() {
				doc, err, rec = d, nil, sung
			}
		}
	}
	if len(doc.Blocks) == 0 {
		return nil, err
	}
	doc.Pub, doc.PubName = t.Pub, pubName
	if rec.Markers != nil && rec.Markers.DocID == doc.DocID && plays(item, rec) {
		doc.Synced = timeLines(doc.Blocks, rec.Markers)
	}
	doc.Downloads = s.resolveDownloads(ctx, lang, doc.Downloads)
	return &doc, nil
}

// recording is the MP3 of a publication's track, which is the rendition
// pub-media names the document of, and the publication's name.
func (s *Service) recording(ctx context.Context, lang string, t track) (model.PubFile, string, error) {
	pm, err := s.PubMedia.Links(ctx, pubmedia.Query{Pub: t.Pub, Issue: t.Issue, Track: t.Track, Formats: []string{"MP3"}, Lang: lang})
	if err != nil {
		return model.PubFile{}, "", err
	}
	files := pm.Files[lang]["MP3"]
	for _, f := range files {
		if f.Track == t.Track {
			return f, pm.PubName, nil
		}
	}
	if len(files) == 1 && files[0].Track == 0 {
		return files[0], pm.PubName, nil
	}
	return model.PubFile{}, pm.PubName, pubmedia.ErrNotFound
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
