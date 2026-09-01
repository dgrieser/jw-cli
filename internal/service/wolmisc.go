package service

import (
	"context"
	"fmt"
	"time"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
)

// DailyText fetches the daily text (Examining the Scriptures Daily) for date.
func (s *Service) DailyText(ctx context.Context, lng model.Language, date time.Time) (model.Article, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return model.Article{}, err
	}
	return s.WOL.DailyText(ctx, cfg, date)
}

// Meetings fetches the overview of the week's meeting material.
func (s *Service) Meetings(ctx context.Context, lng model.Language, date time.Time) (model.Article, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return model.Article{}, err
	}
	return s.WOL.Meetings(ctx, cfg, date)
}

// MeetingPart resolves one meeting's document on the week's material page and
// reads it. which is "midweek" or "weekend".
func (s *Service) MeetingPart(ctx context.Context, lng model.Language, date time.Time, which string) (model.Article, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return model.Article{}, err
	}
	parts, err := s.WOL.MeetingParts(ctx, cfg, date)
	if err != nil {
		return model.Article{}, err
	}
	var target string
	switch which {
	case "midweek":
		target = parts.Midweek
	case "weekend":
		target = parts.Weekend
	default:
		return model.Article{}, fmt.Errorf("unknown meeting %q (want midweek or weekend)", which)
	}
	if target == "" {
		return model.Article{}, fmt.Errorf("no %s material listed for week %d/%d at %s",
			which, parts.Week, parts.Year, parts.URL)
	}
	return s.WOL.DocumentByURL(ctx, target)
}

// MeetingParts exposes the week's meeting-part links themselves.
func (s *Service) MeetingParts(ctx context.Context, lng model.Language, date time.Time) (wol.MeetingParts, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return wol.MeetingParts{}, err
	}
	return s.WOL.MeetingParts(ctx, cfg, date)
}
