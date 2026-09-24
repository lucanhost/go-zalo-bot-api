package zalobot

import (
	"errors"
	"testing"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

func TestPredicatesUseCodeOrHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		fn   func(error) bool
		want bool
	}{
		{"api 401", &APIError{Code: 401}, IsUnauthorized, true},
		{"http 401 decode", &DecodeError{HTTPStatus: 401}, IsUnauthorized, true},
		{"api 429", &APIError{Code: 429}, IsRateLimited, true},
		{"http 429 decode", &DecodeError{HTTPStatus: 429}, IsRateLimited, true},
		{"426 not rate limited", &APIError{Code: 426}, IsRateLimited, false},
		{"426 quota", &APIError{Code: 426}, IsWebhookQuotaExceeded, true},
		{"408 timeout", &APIError{Code: 408}, IsPollingTimeout, true},
		{"wrapped", errors.New("x"), IsUnauthorized, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.err); got != tc.want {
				t.Fatalf("predicate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAliasesResolveToInternalTypes(t *testing.T) {
	var apiErr *api.Error
	if !errors.As(&APIError{Code: 401}, &apiErr) {
		t.Fatal("APIError does not alias api.Error")
	}
	var decodeErr *api.DecodeError
	if !errors.As(&DecodeError{}, &decodeErr) {
		t.Fatal("DecodeError does not alias api.DecodeError")
	}
	var transportErr *api.TransportError
	if !errors.As(&TransportError{}, &transportErr) {
		t.Fatal("TransportError does not alias api.TransportError")
	}
}
