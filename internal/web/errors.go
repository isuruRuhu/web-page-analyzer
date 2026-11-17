package web

import (
	"errors"
	"net/http"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/isuruRuhu/web-page-analyzer/internal/analyzer"
)

// problem is an error as shown on the page.
type problem struct {
	Status  string // code and text, such as "404 Not Found"
	Message string // what went wrong, in full sentences
}

// Outcomes of an analysis, as recorded in page_analyses_total.
const (
	outcomeOK          = "ok"
	outcomeInvalidURL  = "invalid_url"
	outcomeBadStatus   = "bad_status"
	outcomeUnreachable = "unreachable"
	outcomeTimeout     = "timeout"
	outcomeNotHTML     = "not_html"
	outcomeCanceled    = "canceled"
	outcomeError       = "error"
)

// describeError maps an error from Analyze to the outcome recorded in the
// metrics, the status of our response and the problem shown to the user.
// Unexpected errors get a generic message; the details only go to the log.
func describeError(err error) (string, int, *problem) {
	var (
		invalidErr     *analyzer.InvalidURLError
		statusErr      *analyzer.StatusError
		unreachableErr *analyzer.UnreachableError
	)
	switch {
	case errors.As(err, &invalidErr):
		return outcomeInvalidURL, http.StatusBadRequest, newProblem(http.StatusBadRequest,
			sentence(invalidErr.Reason)+" Enter a full URL such as https://example.com.")
	case errors.As(err, &statusErr):
		// The page shows the target's own status; ours says the upstream failed.
		return outcomeBadStatus, http.StatusBadGateway, newProblem(statusErr.StatusCode,
			"The page responded with this error status, so it could not be analyzed.")
	case errors.As(err, &unreachableErr):
		outcome, status := outcomeUnreachable, http.StatusBadGateway
		if unreachableErr.Timeout {
			outcome, status = outcomeTimeout, http.StatusGatewayTimeout
		}
		return outcome, status, newProblem(status, "The page could not be reached: "+unreachableErr.Reason+".")
	case errors.Is(err, analyzer.ErrNotHTML):
		return outcomeNotHTML, http.StatusUnprocessableEntity, newProblem(http.StatusUnprocessableEntity,
			"The URL does not point to an HTML page, so it could not be analyzed.")
	default:
		return outcomeError, http.StatusInternalServerError, newProblem(http.StatusInternalServerError,
			"Something went wrong while analyzing the page. Please try again later.")
	}
}

// newProblem returns a problem for status code, leaving out the text for
// codes that have no standard one.
func newProblem(code int, message string) *problem {
	status := strconv.Itoa(code)
	if text := http.StatusText(code); text != "" {
		status += " " + text
	}
	return &problem{Status: status, Message: message}
}

// sentence capitalizes s and ends it with a full stop.
func sentence(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:] + "."
}
