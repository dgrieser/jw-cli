package service

import (
	"context"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/subtitles"
)

// The search hands a video's quoting passage back as a few lines of its
// transcript, cut where the verse is named — just before what the speaker
// says about it. The transcript itself is the video's subtitles: found in
// it, the passage is widened to the paragraphs it is part of, and to those
// around it, so it reads as what was said about the verse.

// excerptContextWords is how many words of the transcript are added before
// and after the search's passage at the least: whole paragraphs, one at a
// time, until there are as many.
const excerptContextWords = 150

// excerptAnchorWords is how many words at either end of the passage find it
// in the transcript when the passage as a whole is not found word for word.
const excerptAnchorWords = 4

// tagPattern is markup in a search snippet.
var tagPattern = regexp.MustCompile(`<[^>]*>`)

// widenExcerpt is a video's search passage widened from its subtitles, which
// are read through the cache like any page.
func (s *Service) widenExcerpt(ctx context.Context, item model.MediaItem, snippet, ref string) (string, bool) {
	for _, f := range item.Files {
		if f.SubtitlesURL == "" {
			continue
		}
		vtt, err := s.HTTP.GetText(ctx, f.SubtitlesURL, nil)
		if err != nil {
			return "", false
		}
		return transcriptExcerpt(snippet, subtitles.ParseVTT(vtt), ref)
	}
	return "", false
}

// transcriptExcerpt widens snippet, a passage of a video's transcript cues,
// to whole paragraphs and excerptContextWords on either side, as HTML: a
// paragraph a <p>, the verse ref names marked where it is said. Without a
// snippet, the passage is where the transcript first says ref. False when
// the passage is not found in the transcript.
func transcriptExcerpt(snippet string, cues []model.Cue, ref string) (string, bool) {
	if len(cues) == 0 {
		return "", false
	}
	mark := refMarker(ref)
	first, last, ok := passageCues(snippet, cues)
	if !ok && strings.TrimSpace(snippet) == "" && mark != nil {
		// a video found by the verse in its title comes with no passage:
		// where the transcript says the verse first is the passage
		for i, c := range cues {
			if mark.MatchString(c.Text) {
				first, last, ok = i, i, true
				break
			}
		}
	}
	if !ok {
		return "", false
	}

	paras := subtitles.Paragraphs(cues)
	// the paragraph each cue is in
	paraOf := make([]int, 0, len(cues))
	for p, para := range paras {
		for range para {
			paraOf = append(paraOf, p)
		}
	}
	lo, hi := paraOf[first], paraOf[last]
	for added := 0; lo > 0 && added < excerptContextWords; {
		lo--
		added += paraWords(paras[lo])
	}
	for added := 0; hi < len(paras)-1 && added < excerptContextWords; {
		hi++
		added += paraWords(paras[hi])
	}

	var b strings.Builder
	for _, para := range paras[lo : hi+1] {
		texts := make([]string, len(para))
		for i, c := range para {
			texts[i] = c.Text
		}
		text := html.EscapeString(strings.Join(texts, " "))
		if mark != nil {
			text = mark.ReplaceAllString(text, "$1<mark>$2</mark>")
		}
		b.WriteString("<p>" + text + "</p>")
	}
	return b.String(), true
}

// passageCues are the first and last of cues the search's snippet is from.
func passageCues(snippet string, cues []model.Cue) (int, int, bool) {
	want := matchWords(strings.Fields(html.UnescapeString(tagPattern.ReplaceAllString(snippet, " "))))
	if len(want) == 0 {
		return 0, 0, false
	}
	// every word of the transcript, and the cue it is in
	var words []string
	var cueOf []int
	for i, c := range cues {
		for _, w := range matchWords(strings.Fields(c.Text)) {
			words = append(words, w)
			cueOf = append(cueOf, i)
		}
	}
	from, to, ok := locate(words, want)
	if !ok {
		return 0, 0, false
	}
	return cueOf[from], cueOf[to], true
}

// matchWords are words as they are compared: lower case, without the
// punctuation around them — a snippet joins the transcript's lines and may
// quote them differently.
func matchWords(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		w := strings.ToLower(strings.TrimFunc(f, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// locate finds want in words: as a whole, else from where its first words
// are to where its last words are after them. The search may cut a word at
// either end of its passage, so those two are not asked to match.
func locate(words, want []string) (int, int, bool) {
	if i := indexWords(words, want, 0); i >= 0 {
		return i, i + len(want) - 1, true
	}
	if len(want) <= 2 {
		return 0, 0, false
	}
	want = want[1 : len(want)-1]
	if i := indexWords(words, want, 0); i >= 0 {
		return i, i + len(want) - 1, true
	}
	n := min(excerptAnchorWords, len(want))
	from := indexWords(words, want[:n], 0)
	if from < 0 {
		return 0, 0, false
	}
	to := indexWords(words, want[len(want)-n:], from)
	if to < 0 {
		return 0, 0, false
	}
	return from, to + n - 1, true
}

// indexWords is where want starts in words at or after from, or -1.
func indexWords(words, want []string, from int) int {
	if len(want) == 0 {
		return -1
	}
	for i := from; i+len(want) <= len(words); i++ {
		match := true
		for j, w := range want {
			if words[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func paraWords(para []model.Cue) int {
	n := 0
	for _, c := range para {
		n += len(strings.Fields(c.Text))
	}
	return n
}

// chapterVerse is the chapter and verse a reference names: "5:19" of
// "Jakobus 5:19".
var chapterVerse = regexp.MustCompile(`(\d+)\s*:\s*(\d+)`)

// refMarker finds the reference ref where a transcript says it: its chapter
// and verse, with the book named before them and the verses that follow —
// "Jakobus 5:19, 20", "1. Johannes 4:8". nil when ref names no verse. The
// first group is what precedes the reference, the second the reference.
func refMarker(ref string) *regexp.Regexp {
	m := chapterVerse.FindStringSubmatch(ref)
	if m == nil {
		return nil
	}
	return regexp.MustCompile(`(^|[^\p{L}\d:])(` +
		`(?:(?:\d\.?\s*)?\p{Lu}[\p{L}.]*\s+)?` +
		m[1] + `\s*:\s*` + m[2] +
		`(?:\s*[-–,]\s*\d+)*)(?:\b|$)`)
}
