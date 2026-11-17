package dapodik

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// FlexString accepts a JSON null, string, or number. Dapodik returns a mix of
// these for fields such as nipd and tingkat_pendidikan_id.
type FlexString struct {
	Value string
	Valid bool
}

func (f *FlexString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		f.Value = ""
		f.Valid = false
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		f.Value = strings.TrimSpace(s)
		f.Valid = true
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		f.Value = n.String()
		f.Valid = true
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	f.Value = strings.TrimSpace(s)
	f.Valid = true
	return nil
}

func (f FlexString) MarshalJSON() ([]byte, error) {
	if !f.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(f.Value)
}

func (f FlexString) String() string {
	return f.Value
}

func (f FlexString) IsEmpty() bool {
	return !f.Valid || strings.TrimSpace(f.Value) == ""
}

func (f FlexString) Int() (int, bool) {
	if f.IsEmpty() {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(f.Value))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

type Student struct {
	PesertaDidikID      string     `json:"peserta_didik_id"`
	Nama                string     `json:"nama"`
	NISN                FlexString `json:"nisn"`
	NIPD                FlexString `json:"nipd"`
	Rombel              FlexString `json:"rombel"`
	RombelSaatIni       FlexString `json:"rombel_saat_ini"`
	TingkatPendidikanID FlexString `json:"tingkat_pendidikan_id"`
	RegistrasiID        string     `json:"registrasi_id"`
	JenisPendaftaran    FlexString `json:"jenis_pendaftaran_id_str"`
	SoftDelete          FlexString `json:"soft_delete"`
}
