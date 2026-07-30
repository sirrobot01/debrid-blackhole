package manager

import (
	"errors"
	"testing"

	"github.com/sirrobot01/decypharr/internal/customerror"
)

// StreamError sets a Retryable field in three places in stream.go, but until it
// implements IsRetryable() nothing reads it: customerror.IsRetriableError probes
// for that exact method (see the selfRetryable interface in
// internal/customerror/retry.go). Without it every stream error is treated as
// fatal by DownloadWithRetry / downloadChunkWithRetry, so a single transient
// upstream status ends playback for the session.
func TestStreamErrorRetryabilityIsHonoured(t *testing.T) {
	transient := StreamError{
		Err:       errors.New("transient HTTP status: 502"),
		Retryable: true,
	}
	if !customerror.IsRetriableError(transient) {
		t.Error("a retryable StreamError is not reported as retriable: the retry loops will give up and playback dies")
	}

	fatal := StreamError{
		Err:       errors.New("unexpected HTTP status: 416"),
		Retryable: false,
	}
	if customerror.IsRetriableError(fatal) {
		t.Error("a non-retryable StreamError is reported as retriable: the retry loops would spin on a permanent failure")
	}
}
