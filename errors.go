package zalobot

import (
	"errors"
	"fmt"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

type (
	APIError       = api.APIError
	TransportError = api.TransportError
	DecodeError    = api.DecodeError
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("zalobot: invalid %s: %s", e.Field, e.Reason)
}

type WebhookActiveError struct{ URL string }

func (e *WebhookActiveError) Error() string {
	return fmt.Sprintf("zalobot: a webhook is active (%s); call DeleteWebhook or set WithAutoDeleteWebhook(true)", e.URL)
}

var (
	ErrNoToken           = errors.New("zalobot: bot token is empty")
	ErrStopped           = errors.New("zalobot: bot is stopped")
	ErrAlreadyPolling    = errors.New("zalobot: bot is already polling")
	ErrQueueFull         = errors.New("zalobot: update queue is full")
	ErrSubstringNotFound = errors.New("zalobot: substring not found")
	ErrUpdatesDropped    = errors.New("zalobot: updates channel buffer full; update dropped for Updates() consumers")
)

func apiCode(err error) (int, bool) {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code, true
	}
	return 0, false
}

func httpStatus(err error) (int, bool) {
	var ae *APIError
	if errors.As(err, &ae) && ae.HTTPStatus != 0 {
		return ae.HTTPStatus, true
	}
	var de *DecodeError
	if errors.As(err, &de) && de.HTTPStatus != 0 {
		return de.HTTPStatus, true
	}
	return 0, false
}

func matchesStatus(err error, status int) bool {
	if c, ok := apiCode(err); ok && c == status {
		return true
	}
	if s, ok := httpStatus(err); ok && s == status {
		return true
	}
	return false
}

func IsUnauthorized(err error) bool         { return matchesStatus(err, 401) }
func IsRateLimited(err error) bool          { return matchesStatus(err, 429) }
func IsPollingTimeout(err error) bool       { return matchesStatus(err, 408) }
func IsWebhookQuotaExceeded(err error) bool { return matchesStatus(err, 426) }
