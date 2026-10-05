package service

import (
	"slices"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

func lanks(media []model.MediaItem) []string {
	var out []string
	for _, m := range media {
		out = append(out, m.LANK)
	}
	return out
}

func items(keys ...string) []model.MediaItem {
	var out []model.MediaItem
	for _, k := range keys {
		out = append(out, model.MediaItem{LANK: k, Type: "audio"})
	}
	return out
}

func files(tracks ...int) []model.PubFile {
	var out []model.PubFile
	for _, t := range tracks {
		out = append(out, model.PubFile{Track: t, Title: "t", URL: "u", Duration: 177.03, Modified: "2026-10-01 20:46:48"})
	}
	return out
}

// Tracks the mediator lacks take their place among the publication's others,
// the list's direction kept; described tracks join only a list that has some.
func TestCompleteAudio(t *testing.T) {
	cases := []struct {
		name   string
		media  []model.MediaItem
		tracks map[string][]model.PubFile
		want   []string
	}{
		{
			name:   "new tracks after the last",
			media:  items("pub-imc_1_AUDIO", "pub-imc_2_AUDIO"),
			tracks: map[string][]model.PubFile{"imc": files(1, 2, 3, 4, 516)},
			want:   []string{"pub-imc_1_AUDIO", "pub-imc_2_AUDIO", "pub-imc_3_AUDIO", "pub-imc_4_AUDIO"},
		},
		{
			name:   "a gap filled in place",
			media:  items("pub-sjjc_130_AUDIO", "pub-sjjc_132_AUDIO", "other"),
			tracks: map[string][]model.PubFile{"sjjc": files(130, 131, 132)},
			want:   []string{"pub-sjjc_130_AUDIO", "pub-sjjc_131_AUDIO", "pub-sjjc_132_AUDIO", "other"},
		},
		{
			name:   "counting down",
			media:  items("x", "pub-pk_502_AUDIO", "pub-pk_12_AUDIO", "pub-pks_1_AUDIO"),
			tracks: map[string][]model.PubFile{"pk": files(12, 13, 502, 503)},
			want:   []string{"x", "pub-pk_503_AUDIO", "pub-pk_502_AUDIO", "pub-pk_13_AUDIO", "pub-pk_12_AUDIO", "pub-pks_1_AUDIO"},
		},
		{
			name:   "a publication pub-media cannot read stays as it is",
			media:  items("pub-iam-4_1_AUDIO"),
			tracks: map[string][]model.PubFile{},
			want:   []string{"pub-iam-4_1_AUDIO"},
		},
	}
	for _, c := range cases {
		got := lanks(completeAudio(c.media, c.tracks, "Cat"))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// A publication whose listed tracks share one picture lends it to the new ones.
func TestCompleteAudioCover(t *testing.T) {
	cover := map[string]map[string]string{"sqr": {"md": "cover.jpg"}}
	media := items("pub-imc_1_AUDIO", "pub-imc_2_AUDIO")
	for i := range media {
		media[i].Images = cover
	}
	got := completeAudio(media, map[string][]model.PubFile{"imc": files(3)}, "Cat")
	if len(got) != 3 || BestImage(got[2].Images) != "cover.jpg" {
		t.Fatalf("%+v", got)
	}
	media[1].Images = map[string]map[string]string{"sqr": {"md": "song.jpg"}}
	got = completeAudio(media[:2:2], map[string][]model.PubFile{"imc": files(3)}, "Cat")
	if got[2].Images != nil {
		t.Fatalf("art of single songs lent: %+v", got[2].Images)
	}
}

func TestTrackItem(t *testing.T) {
	it := trackItem("iam-4", model.PubFile{Track: 3, Title: "Song", URL: "u.mp3", MimeType: "audio/mpeg", Filesize: 9, Duration: 177.03, Modified: "2026-10-01 20:46:48"}, "Cat")
	if it.LANK != "pub-iam-4_3_AUDIO" || it.DurationFormatted != "2m 57s" || it.FirstPublished != "2026-10-01T20:46:48Z" || it.PrimaryCategory != "Cat" || len(it.Files) != 1 || it.Files[0].URL != "u.mp3" {
		t.Fatalf("%+v", it)
	}
	if sym, track, ok := audioTrack(it.LANK); !ok || sym != "iam-4" || track != 3 {
		t.Fatalf("audioTrack: %q %d %v", sym, track, ok)
	}
}
