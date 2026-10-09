package subtitles

import (
	"reflect"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

func TestParseVTT(t *testing.T) {
	src := "WEBVTT\r\n\r\nNOTE a comment --\r\n\r\n" +
		"00:00:04.108 --> 00:00:07.236 line:90% position:50% align:center\r\nSince its release in 2021,\r\n\r\n" +
		"cue-2\n00:07.236 --> 00:09.780\nmillions of people have studied\nthe publication\n\n" +
		"00:00:09.780 --> 00:00:12.658\n<i>Enjoy Life Forever! </i>\n\n" +
		"00:00:12.700 --> 00:00:13.000\n<c.yellow></c>\n\n" +
		"01:00:00.000 --> 01:00:01.500\nTom &amp; Jerry\n"
	want := []model.Cue{
		{Start: 4.108, End: 7.236, Text: "Since its release in 2021,"},
		{Start: 7.236, End: 9.78, Text: "millions of people have studied the publication"},
		{Start: 9.78, End: 12.658, Text: "Enjoy Life Forever!"},
		{Start: 3600, End: 3601.5, Text: "Tom & Jerry"},
	}
	if got := ParseVTT(src); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseVTT:\n got %+v\nwant %+v", got, want)
	}
}

func TestClock(t *testing.T) {
	for sec, want := range map[float64]string{0: "0:00", 4.9: "0:04", 75: "1:15", 3723: "1:02:03", -2: "0:00"} {
		if got := Clock(sec); got != want {
			t.Errorf("Clock(%v) = %q, want %q", sec, got, want)
		}
	}
}

// A paragraph ends at a sentence's end followed by a pause, never inside a
// sentence however long the pause.
func TestParagraphs(t *testing.T) {
	cues := []model.Cue{
		{Start: 0, End: 2, Text: "First sentence."},
		{Start: 2.1, End: 4, Text: "Second one goes"},
		{Start: 9, End: 10, Text: "on after a pause."},
		{Start: 12, End: 13, Text: "“A new thought.”"},
		{Start: 13.2, End: 14, Text: "Same breath."},
	}
	var got [][]string
	for _, p := range Paragraphs(cues) {
		var texts []string
		for _, c := range p {
			texts = append(texts, c.Text)
		}
		got = append(got, texts)
	}
	want := [][]string{
		{"First sentence.", "Second one goes", "on after a pause."},
		{"“A new thought.”", "Same breath."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Paragraphs = %q, want %q", got, want)
	}
	if Paragraphs(nil) != nil {
		t.Error("no cues, no paragraphs")
	}
}
