package dapodik

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type AuthError struct {
	Message string
}

func (e *AuthError) Error() string { return e.Message }

type APIError struct {
	Message string
	Status  int
}

func (e *APIError) Error() string { return e.Message }

type Config struct {
	BaseURL   string
	Cookie    string
	Token     string
	UserAgent string
	Timeout   time.Duration
	Retries   int
}

type Client struct {
	baseURL   string
	cookie    string
	token     string
	userAgent string
	timeout   time.Duration
	retries   int
	http      *http.Client
}

func New(cfg Config) *Client {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	retries := cfg.Retries
	if retries < 0 {
		retries = 0
	}
	ua := strings.TrimSpace(cfg.UserAgent)
	if ua == "" {
		ua = "dapodik-tool/1.3.1"
	}
	return &Client{
		baseURL:   base,
		cookie:    normalizeCookie(cfg.Cookie),
		token:     strings.TrimSpace(cfg.Token),
		userAgent: ua,
		timeout:   timeout,
		retries:   retries,
		http:      &http.Client{Timeout: timeout},
	}
}

func normalizeCookie(raw string) string {
	cookie := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(cookie), "cookie:") {
		cookie = strings.TrimSpace(cookie[len("cookie:"):])
	}
	lines := strings.Split(cookie, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, " ")
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (int, []byte, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("User-Agent", c.userAgent)
		if c.cookie != "" {
			req.Header.Set("Cookie", c.cookie)
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if attempt < c.retries {
				if err := sleepCtx(ctx, time.Duration(attempt+1)*1500*time.Millisecond); err != nil {
					return 0, nil, err
				}
				continue
			}
			return 0, nil, fmt.Errorf("koneksi gagal ke %s: %w", u, lastErr)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if readErr != nil {
			return resp.StatusCode, nil, fmt.Errorf("membaca respons gagal: %w", readErr)
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return resp.StatusCode, data, &AuthError{Message: fmt.Sprintf("HTTP %d dari %s. Cookie/token kemungkinan sudah tidak berlaku.", resp.StatusCode, u)}
		}
		if (resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests) && attempt < c.retries {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if err := sleepCtx(ctx, time.Duration(attempt+1)*1500*time.Millisecond); err != nil {
				return resp.StatusCode, data, err
			}
			continue
		}
		return resp.StatusCode, data, nil
	}
	return 0, nil, lastErr
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type FetchProgress struct {
	Page      int
	Start     int
	Rows      int
	Collected int
	Total     *int
}

type FetchOptions struct {
	SekolahID string
	NPSN      string
	PDModule  string
	Limit     int
	Delay     time.Duration
	MaxPages  int
	Progress  func(FetchProgress)
}

func (c *Client) FetchStudents(ctx context.Context, opts FetchOptions) ([]Student, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20000
	}
	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}
	module := strings.TrimSpace(opts.PDModule)
	if module == "" {
		module = "pdterdaftar"
	}

	var students []Student
	seen := make(map[string]struct{})
	page, start := 1, 0

	for page <= maxPages {
		q := url.Values{}
		q.Set("_dc", strconv.FormatInt(time.Now().UnixMilli(), 10))
		q.Set("sekolah_id", opts.SekolahID)
		q.Set("pd_module", module)
		q.Set("limit", strconv.Itoa(limit))
		q.Set("ascending", "nama")
		q.Set("page", strconv.Itoa(page))
		q.Set("start", strconv.Itoa(start))
		if opts.NPSN != "" {
			q.Set("npsn", opts.NPSN)
		}

		status, body, err := c.do(ctx, http.MethodGet, "/rest/PesertaDidik", q, nil)
		if err != nil {
			return nil, err
		}
		rows, total, err := decodeStudents(status, body)
		if err != nil {
			return nil, err
		}

		added := 0
		for _, s := range rows {
			key := strings.TrimSpace(s.PesertaDidikID)
			if key == "" {
				key = strings.TrimSpace(s.RegistrasiID) + "|" + strings.TrimSpace(s.NISN.Value)
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			students = append(students, s)
			added++
		}

		if opts.Progress != nil {
			opts.Progress(FetchProgress{
				Page: page, Start: start, Rows: len(rows),
				Collected: len(students), Total: total,
			})
		}

		if len(rows) == 0 || added == 0 {
			break
		}
		if total != nil && len(students) >= *total {
			break
		}
		start += len(rows)
		page++
		if opts.Delay > 0 {
			if err := sleepCtx(ctx, opts.Delay); err != nil {
				return nil, err
			}
		}
	}
	return students, nil
}

func (c *Client) UpdateNIPD(ctx context.Context, registrasiID, nipd string) error {
	path := "/rest/RegistrasiPesertaDidik/" + url.PathEscape(strings.TrimSpace(registrasiID))
	q := url.Values{}
	q.Set("_dc", strconv.FormatInt(time.Now().UnixMilli(), 10))
	payload := map[string]string{
		"nipd":          nipd,
		"registrasi_id": registrasiID,
	}
	status, body, err := c.do(ctx, http.MethodPut, path, q, payload)
	if err != nil {
		return err
	}
	return decodeUpdate(status, body)
}

var _ = io.Discard
