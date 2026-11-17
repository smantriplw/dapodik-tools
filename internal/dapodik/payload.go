package dapodik

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type apiResponse struct {
	Success *bool           `json:"success"`
	Message string          `json:"message"`
	Rows    json.RawMessage `json:"rows"`
	Data    json.RawMessage `json:"data"`
	Total   *int            `json:"total"`
}

func looksLikeAuthMessage(msg string) bool {
	l := strings.ToLower(msg)
	if !strings.Contains(l, "session") {
		return false
	}
	return strings.Contains(l, "habis") || strings.Contains(l, "login") ||
		strings.Contains(l, "expired") || strings.Contains(l, "kedaluwarsa")
}

func phpMessage(body []byte) string {
	// Dapodik sometimes replies with PHP-style arrays:
	// { 'success' : false, 'message' : 'Session telah habis...' }
	re := regexp.MustCompile(`(?i)['"]message['"]\s*:\s*['"]([^'"]*)['"]`)
	if m := re.FindSubmatch(body); len(m) == 2 {
		return strings.TrimSpace(string(m[1]))
	}
	return ""
}

var phpSuccessTrue = regexp.MustCompile(`(?i)['"]success['"]\s*:\s*(true|1)\b`)

func fallbackError(status int, body []byte) error {
	msg := phpMessage(body)
	if msg == "" {
		snippet := strings.Join(strings.Fields(string(body)), " ")
		if len(snippet) > 240 {
			snippet = snippet[:240]
		}
		msg = fmt.Sprintf("respons tidak dikenali (HTTP %d): %s", status, snippet)
	} else {
		msg = fmt.Sprintf("%s (HTTP %d)", msg, status)
	}
	if looksLikeAuthMessage(msg) {
		return &AuthError{Message: msg}
	}
	return &APIError{Message: msg, Status: status}
}

func decodeStudents(status int, body []byte) ([]Student, *int, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil, &APIError{Message: fmt.Sprintf("respons kosong (HTTP %d)", status), Status: status}
	}
	if body[0] == '[' {
		var rows []Student
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, nil, fallbackError(status, body)
		}
		return rows, nil, nil
	}

	var envelope apiResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, fallbackError(status, body)
	}
	if envelope.Success != nil && !*envelope.Success {
		msg := strings.TrimSpace(envelope.Message)
		if msg == "" {
			msg = "permintaan ditolak server"
		}
		if looksLikeAuthMessage(msg) {
			return nil, nil, &AuthError{Message: msg}
		}
		return nil, nil, &APIError{Message: msg, Status: status}
	}

	raw := envelope.Rows
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = envelope.Data
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, envelope.Total, nil
	}
	if bytes.TrimSpace(raw)[0] == '{' {
		var single Student
		if err := json.Unmarshal(raw, &single); err != nil {
			return nil, nil, fallbackError(status, raw)
		}
		return []Student{single}, envelope.Total, nil
	}
	var rows []Student
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, nil, fallbackError(status, raw)
	}
	return rows, envelope.Total, nil
}

func decodeUpdate(status int, body []byte) error {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		if status >= 200 && status < 300 {
			return nil
		}
		return &APIError{Message: fmt.Sprintf("respons kosong (HTTP %d)", status), Status: status}
	}

	var envelope apiResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		msg := phpMessage(body)
		if phpSuccessTrue.Match(body) || strings.Contains(strings.ToLower(msg), "berhasil") {
			return nil
		}
		return fallbackError(status, body)
	}
	msg := strings.TrimSpace(envelope.Message)
	if envelope.Success != nil {
		if *envelope.Success {
			if msg == "" {
				msg = "Berhasil mengupdate RegistrasiPesertaDidik"
			}
			return nil
		}
		if msg == "" {
			msg = "update ditolak server"
		}
		if looksLikeAuthMessage(msg) {
			return &AuthError{Message: msg}
		}
		return &APIError{Message: msg, Status: status}
	}
	if status >= 200 && status < 300 {
		return nil
	}
	return fallbackError(status, body)
}
