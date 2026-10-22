package server

import (
	"context"
	"time"
)

// StudyWeek studies the reading of the week containing date at once,
// without waiting between requests.
func (s *Server) StudyWeek(ctx context.Context, date time.Time) {
	defer func(p time.Duration) { studyPace = p }(studyPace)
	studyPace = 0
	s.studyWeek(ctx, date)
}
