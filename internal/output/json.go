package output

import (
	"encoding/json"
	"io"
)

type Envelope struct {
	OK      bool           `json:"ok"`
	Data    any            `json:"data,omitempty"`
	Code    string         `json:"code,omitempty"`
	Message string         `json:"msg,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

func Success(data any) Envelope {
	return Envelope{OK: true, Data: data}
}

func Failure(code, message string, details map[string]any) Envelope {
	return Envelope{OK: false, Code: code, Message: message, Details: details}
}

func Marshal(envelope Envelope) ([]byte, error) {
	return json.Marshal(envelope)
}

func Write(w io.Writer, envelope Envelope) error {
	data, err := Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
