package service

import (
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// A video or recording is named by its series and year, as a publication by
// its symbol: the issue its key carries left out, a document's media named by
// their category.
func TestMediaCode(t *testing.T) {
	for _, c := range []struct {
		lank, category, published, want string
	}{
		{"pub-jwb-136_5_VIDEO", "", "2026-04-07T00:00:00.000Z", "vi-jwb26"},
		{"pub-jwb_201801_12_VIDEO", "", "2018-02-19T00:00:00.000Z", "vi-jwb18"},
		{"pub-co-r22_147_VIDEO", "", "2022-08-15T00:00:00.000Z", "vi-co-r22"},
		{"pub-jwbcov22_3_VIDEO", "", "2022-09-13T00:00:00.000Z", "vi-jwbcov22"},
		{"pub-mwbv_201906_3_VIDEO", "", "2019-05-15T00:00:00.000Z", "vi-mwbv19"},
		{"pub-sjjm_1_AUDIO", "", "2017-10-30T00:00:00.000Z", "au-sjjm17"},
		{"pub-iam-1_10_AUDIO", "", "2009-09-30T00:00:00.000Z", "au-iam09"},
		{"pub-t-famv_101_VIDEO", "", "2025-10-04T00:00:00.000Z", "vi-t-famv25"},
		{"pub-ivdd_x_VIDEO", "", "2013-08-26T00:00:00.000Z", "vi-ivdd13"},
		{"pub-sjjm_VIDEO", "", "", "vi-sjjm"},
		{"docid-502100035_1_VIDEO", "SeriesWhiteboard", "2026-08-31T00:00:00.000Z", "vi-wba26"},
		{"docid-502100006_101_VIDEO", "SeriesWhiteboardAD", "2024-11-22T00:00:00.000Z", "vi-wba24"},
		{"docid-1112024060_1_VIDEO", "StudioNewsReports", "2026-09-18T00:00:00.000Z", "vi-lb26"},
		{"docid-1_1_VIDEO", "SomethingNew", "1998-01-01T00:00:00.000Z", "vi-jw98"},
	} {
		item := model.MediaItem{LANK: c.lank, PrimaryCategory: c.category, FirstPublished: c.published}
		if got := MediaCode(item); got != c.want {
			t.Errorf("%s: got %q, want %q", c.lank, got, c.want)
		}
	}
}
