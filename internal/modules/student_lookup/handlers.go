package student_lookup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"dapodik-tool/internal/dapodik"
	"dapodik-tool/internal/registry"
)

type searchRequest struct {
	ScopeID string `json:"scope_id"`
	Limit   int    `json:"limit"`
	NISN    string `json:"nisn"`
}

type studentResult struct {
	Nama         string `json:"nama"`
	NISN         string `json:"nisn"`
	Rombel       string `json:"rombel"`
	Tingkat      string `json:"tingkat"`
	NIPD         string `json:"nipd"`
	RegistrasiID string `json:"registrasi_id"`
	HasRombel    bool   `json:"has_rombel"`
	NIPDEmpty    bool   `json:"nipd_empty"`
}

type searchResponse struct {
	Results       []studentResult `json:"results"`
	Missing       []string        `json:"missing"`
	TotalStudents int             `json:"total_students"`
	FetchedAt     string          `json:"fetched_at"`
	FromCache     bool            `json:"from_cache"`
}

type cacheEntry struct {
	students []dapodik.Student
	total    int
	expires  time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

var nisnPattern = regexp.MustCompile(`^[0-9]{4,20}$`)

func searchHandler(s *registry.Services) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req searchRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Body JSON tidak valid."})
			return
		}
		if s.Scopes == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Scope Dapodik belum tersedia."})
			return
		}
		creds, err := s.Scopes.Resolve(s.CurrentUser(r), strings.TrimSpace(req.ScopeID))
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, registry.ErrScopeNotAllowed) {
				status = http.StatusForbidden
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		if creds.Cookie == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Scope " + creds.Name + " belum memiliki cookie."})
			return
		}
		nisns := parseNISNList(req.NISN)
		if len(nisns) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Masukkan minimal satu NISN."})
			return
		}
		if len(nisns) > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Maksimal 100 NISN per pencarian."})
			return
		}
		for _, n := range nisns {
			if !nisnPattern.MatchString(n) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "NISN tidak valid: " + n})
				return
			}
		}
		if req.Limit <= 0 {
			req.Limit = 20000
		}
		if req.Limit > 50000 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Limit maksimal 50000."})
			return
		}

		key := cacheKey(creds, req.Limit)
		now := time.Now()
		var students []dapodik.Student
		fromCache := false

		cacheMu.Lock()
		for k, v := range cache {
			if now.After(v.expires) {
				delete(cache, k)
			}
		}
		if entry, ok := cache[key]; ok && now.Before(entry.expires) {
			students = entry.students
			fromCache = true
		}
		cacheMu.Unlock()

		if students == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
			defer cancel()
			client := dapodik.New(dapodik.Config{
				BaseURL: creds.BaseURL,
				Cookie:  creds.Cookie,
				Token:   creds.Token,
				Timeout: 120 * time.Second,
				Retries: 1,
			})
			fetched, err := client.FetchStudents(ctx, dapodik.FetchOptions{
				SekolahID: creds.SekolahID,
				NPSN:      creds.NPSN,
				PDModule:  "pdterdaftar",
				Limit:     req.Limit,
				MaxPages:  50,
			})
			if err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
				return
			}
			students = fetched
			cacheMu.Lock()
			cache[key] = cacheEntry{students: students, total: len(students), expires: now.Add(5 * time.Minute)}
			cacheMu.Unlock()
		}

		index := make(map[string]dapodik.Student, len(students))
		for _, st := range students {
			nisn := strings.TrimSpace(st.NISN.Value)
			if nisn == "" {
				continue
			}
			if _, exists := index[nisn]; !exists {
				index[nisn] = st
			}
		}

		resp := searchResponse{
			TotalStudents: len(students),
			FetchedAt:     time.Now().UTC().Format(time.RFC3339),
			FromCache:     fromCache,
		}
		for _, nisn := range nisns {
			st, ok := index[nisn]
			if !ok {
				resp.Missing = append(resp.Missing, nisn)
				continue
			}
			rombel := strings.TrimSpace(st.Rombel.Value)
			resp.Results = append(resp.Results, studentResult{
				Nama:         strings.TrimSpace(st.Nama),
				NISN:         nisn,
				Rombel:       rombel,
				Tingkat:      strings.TrimSpace(st.TingkatPendidikanID.Value),
				NIPD:         strings.TrimSpace(st.NIPD.Value),
				RegistrasiID: strings.TrimSpace(st.RegistrasiID),
				HasRombel:    rombel != "",
				NIPDEmpty:    st.NIPD.IsEmpty(),
			})
		}
		s.Audit(r, "student_lookup", map[string]any{
			"scope_id": creds.ID, "nisn_count": len(nisns),
			"found": len(resp.Results), "missing": len(resp.Missing),
		})
		writeJSON(w, http.StatusOK, resp)
	}
}

func parseNISNList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\r' || r == '\t'
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func cacheKey(creds registry.ScopeCredentials, limit int) string {
	h := sha256.New()
	_, _ = io.WriteString(h, creds.ID+"|"+creds.BaseURL+"|"+creds.SekolahID+"|"+creds.NPSN+"|"+strconv.Itoa(limit))
	_, _ = io.WriteString(h, "|"+creds.Cookie+"|"+creds.Token)
	return hex.EncodeToString(h.Sum(nil))
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
