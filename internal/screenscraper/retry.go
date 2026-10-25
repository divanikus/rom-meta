package screenscraper

import (
	"errors"
	"net"
	"time"
)

// Search pacing belongs to the import, so it does not reset between games.
type searchPacer struct {
	interval time.Duration
	last     time.Time
}

func newSearchPacer(rps int) *searchPacer {
	if rps <= 0 {
		rps = defaultRateLimitRPS
	}
	return &searchPacer{interval: time.Second / time.Duration(rps)}
}

func (p *searchPacer) wait() {
	if delay := time.Until(p.last.Add(p.interval)); delay > 0 {
		time.Sleep(delay)
	}
	p.last = time.Now()
}

func isTimeout(err error) bool {
	var timeout net.Error
	return errors.As(err, &timeout) && timeout.Timeout()
}
