package api

import "fmt"

type APIError struct {
	Code        int
	Description string
	Method      string
	HTTPStatus  int
}

func (e *APIError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("zalobot: %s failed: %d %s", e.Method, e.Code, e.Description)
	}
	return fmt.Sprintf("zalobot: %s failed: %d", e.Method, e.Code)
}

type TransportError struct {
	Method      string
	RedactedURL string
	Err         error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("zalobot: %s transport error (%s): %v", e.Method, e.RedactedURL, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

type DecodeError struct {
	Method     string
	HTTPStatus int
	Err        error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("zalobot: %s: cannot decode response (HTTP %d): %v", e.Method, e.HTTPStatus, e.Err)
}

func (e *DecodeError) Unwrap() error { return e.Err }
