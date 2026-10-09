// Package subtitles reads the WebVTT files jw.org's videos are subtitled
// with into a transcript: the captions in order, each with its time.
package subtitles

import (
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
)

// tags are the cue's own markup — <i>, <b>, <c.yellow>, <v Speaker>, a
// karaoke timestamp <00:01.000> — which a transcript has no use for.
var tags = regexp.MustCompile(`<[^>]*>`)

// ParseVTT reads a WebVTT file's cues. Notes, styles and regions are passed
// over, as is a cue without words. A caption broken over several lines is
// one line again: the breaks are the picture's, not the speaker's.
func ParseVTT(src string) []model.Cue {
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	var cues []model.Cue
	for block := range strings.SplitSeq(src, "\n\n") {
		lines := strings.Split(strings.Trim(block, "\n"), "\n")
		// the timing line, after an optional identifier
		at := -1
		for i, l := range lines {
			if strings.Contains(l, "-->") {
				at = i
				break
			}
			if i > 0 {
				break
			}
		}
		if at < 0 {
			continue
		}
		start, end, ok := timing(lines[at])
		if !ok {
			continue
		}
		var words []string
		for _, l := range lines[at+1:] {
			l = strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(l, "")))
			if l != "" {
				words = append(words, strings.Join(strings.Fields(l), " "))
			}
		}
		if len(words) == 0 {
			continue
		}
		cues = append(cues, model.Cue{Start: start, End: end, Text: strings.Join(words, " ")})
	}
	return cues
}

// timing reads "00:00:04.108 --> 00:00:07.236 line:90% align:center".
func timing(line string) (float64, float64, bool) {
	from, rest, ok := strings.Cut(line, "-->")
	if !ok {
		return 0, 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, 0, false
	}
	start, ok1 := Seconds(strings.TrimSpace(from))
	end, ok2 := Seconds(fields[0])
	return start, end, ok1 && ok2
}

// Seconds reads a WebVTT timestamp, "01:02:03.456" or "02:03.456".
func Seconds(ts string) (float64, bool) {
	parts := strings.Split(ts, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.Replace(p, ",", ".", 1), 64)
		if err != nil || v < 0 {
			return 0, false
		}
		total = total*60 + v
	}
	return total, true
}

// Clock writes seconds the way a player shows them: 4:07, 1:02:03.
func Clock(sec float64) string {
	t := max(int(sec), 0)
	if t >= 3600 {
		return strconv.Itoa(t/3600) + ":" + pad(t/60%60) + ":" + pad(t%60)
	}
	return strconv.Itoa(t/60) + ":" + pad(t%60)
}

func pad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// Paragraphs joins a transcript's captions into paragraphs of running text. A
// caption ending a sentence ends the paragraph when the next one is a pause
// away, or when the paragraph has grown long; one that does not end a
// sentence never does, so a sentence is not torn apart.
func Paragraphs(cues []model.Cue) [][]model.Cue {
	const (
		pause = 1.0 // seconds of silence that end a thought
		long  = 600 // characters a paragraph may run before it ends at the next sentence
	)
	var out [][]model.Cue
	var cur []model.Cue
	size := 0
	for i, c := range cues {
		cur = append(cur, c)
		size += len(c.Text) + 1
		if i == len(cues)-1 {
			break
		}
		if sentenceEnd(c.Text) && (cues[i+1].Start-c.End >= pause || size >= long) {
			out = append(out, cur)
			cur, size = nil, 0
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// sentenceEnd says the text ends a sentence, closing quotes and brackets
// aside.
func sentenceEnd(s string) bool {
	s = strings.TrimRight(s, " \"'”’»)]")
	if s == "" {
		return false
	}
	switch r := []rune(s); r[len(r)-1] {
	case '.', '!', '?', '…', '。', '！', '？', '।', '؟':
		return true
	}
	return false
}
