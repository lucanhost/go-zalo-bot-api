package api

import "encoding/json"

type envelope struct {
	OK             bool            `json:"ok"`
	Result         json.RawMessage `json:"result"`
	Description    string          `json:"description"`
	ErrorCode      int             `json:"error_code"`
	ErrorCodeCamel int             `json:"errorCode"`
}
