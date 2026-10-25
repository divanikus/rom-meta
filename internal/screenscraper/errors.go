package screenscraper

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrDailyQuota stops this provider for the current run. Successful writes remain
// resumable after ScreenScraper resets its daily allowance.
var ErrDailyQuota = errors.New("ScreenScraper daily quota exceeded (HTTP 430); resume after the quota resets; saved metadata and artwork will be kept")

// Keep server responses separate from transport errors: request URLs can contain
// arbitrary titles, hashes and credentials that look like authentication errors.
type searchResponseError struct {
	statusCode int
	message    string
}

func (e *searchResponseError) Error() string {
	if e.statusCode >= 200 && e.statusCode < 300 {
		return "ScreenScraper search returned: " + e.message
	}
	return fmt.Sprintf("ScreenScraper search returned %d %s: %s", e.statusCode, http.StatusText(e.statusCode), e.message)
}

func isFatalSearchError(err error) bool {
	if errors.Is(err, ErrDailyQuota) {
		return true
	}
	var response *searchResponseError
	if !errors.As(err, &response) {
		return false
	}
	if response.statusCode == http.StatusUnauthorized || response.statusCode == http.StatusForbidden {
		return true
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		return false
	}
	// ScreenScraper can also report authentication failures as HTTP 200 text.
	message := strings.ToLower(response.message)
	return strings.Contains(message, "login") || strings.Contains(message, "identifiants")
}

func isNotFoundError(err error) bool {
	var response *searchResponseError
	return errors.As(err, &response) &&
		response.statusCode == http.StatusNotFound &&
		isScreenScraperNotFoundMessage(response.message)
}

// ScreenScraper puts credentials in the request URL. Retain the underlying
// cause (including timeout identity) without including that URL in diagnostics.
func withoutRequestURL(err error) error {
	if requestError, ok := err.(*url.Error); ok {
		return withoutRequestURL(requestError.Err)
	}
	return err
}
