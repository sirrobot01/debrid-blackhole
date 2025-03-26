package request

type HTTPError struct {
	StatusCode int
	Message    string
	Code       string
}

func (e *HTTPError) Error() string {
	return e.Message
}

var HosterUnavailableError = &HTTPError{
	StatusCode: 503,
	Message:    "Hoster is unavailable",
	Code:       "hoster_unavailable",
}

var ErrLinkBroken = &HTTPError{
	StatusCode: 404,
	Message:    "File is unavailable",
	Code:       "file_unavailable",
}
