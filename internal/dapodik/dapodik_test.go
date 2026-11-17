package dapodik

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func flex(s string) FlexString { return FlexString{Value: s, Valid: true} }

func TestGroupOrderAndNoRombel(t *testing.T) {
	students := []Student{
		{Nama: "a", Rombel: flex("XII A2"), NIPD: flex("20"), RegistrasiID: "1"},
		{Nama: "b", Rombel: flex("X B"), NIPD: flex("21"), RegistrasiID: "2"},
		{Nama: "c", Rombel: flex("X A"), NIPD: flex("22"), RegistrasiID: "3"},
		{Nama: "d", Rombel: flex("XI C4"), NIPD: flex("23"), RegistrasiID: "4"},
		{Nama: "e", Rombel: FlexString{}, NIPD: FlexString{}, RegistrasiID: "5"},
		{Nama: "f", Rombel: flex("X A"), NIPD: FlexString{}, RegistrasiID: "6"},
	}
	plan, err := BuildPlan(students, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"X A", "X B", "XI C4", "XII A2", NoRombelLabel}
	if len(plan.Groups) != len(want) {
		t.Fatalf("groups = %d, want %d", len(plan.Groups), len(want))
	}
	for i, label := range want {
		if plan.Groups[i].Label != label {
			t.Fatalf("group[%d] = %q, want %q", i, plan.Groups[i].Label, label)
		}
	}
	if plan.NoRombel != 1 {
		t.Fatalf("no rombel = %d, want 1", plan.NoRombel)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(plan.Items))
	}
	if plan.Items[0].Group != "X A" || plan.Items[1].Group != NoRombelLabel {
		t.Fatalf("unexpected item order: %+v", plan.Items)
	}
	if plan.Items[0].NewNIPD != "24" || plan.Items[1].NewNIPD != "25" {
		t.Fatalf("unexpected NIPD assignment: %+v", plan.Items)
	}
}

func TestPlanSelectedGroupsAndCollision(t *testing.T) {
	start := 52
	students := []Student{
		{Nama: "A", Rombel: flex("X A"), NIPD: flex("40"), RegistrasiID: "a"},
		{Nama: "B", Rombel: flex("X A"), NIPD: FlexString{}, RegistrasiID: "b"},
		{Nama: "C", Rombel: flex("X B"), NIPD: FlexString{}, RegistrasiID: "c"},
		{Nama: "D", Rombel: flex("X B"), NIPD: flex("abc"), RegistrasiID: "d"},
		{Nama: "E", Rombel: flex("X B"), NIPD: FlexString{}, RegistrasiID: "e"},
		{Nama: "F", Rombel: flex("XI A"), NIPD: flex("52"), RegistrasiID: "f"},
	}
	plan, err := BuildPlan(students, PlanOptions{
		StartNIPD:      &start,
		SelectedGroups: map[string]bool{"X B": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HighestNIPD != 52 {
		t.Fatalf("highest = %d, want 52", plan.HighestNIPD)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(plan.Items))
	}
	if plan.Items[0].RegistrasiID != "c" || plan.Items[0].NewNIPD != "53" {
		t.Fatalf("first item = %+v", plan.Items[0])
	}
	if plan.Items[1].RegistrasiID != "e" || plan.Items[1].NewNIPD != "54" {
		t.Fatalf("second item = %+v", plan.Items[1])
	}
	if plan.Filled != 3 || plan.Empty != 3 {
		t.Fatalf("filled/empty = %d/%d, want 3/3", plan.Filled, plan.Empty)
	}
}

func TestPlanRequiresStartWhenNoNumeric(t *testing.T) {
	students := []Student{{Nama: "A", NIPD: flex("x"), RegistrasiID: "a"}}
	if _, err := BuildPlan(students, PlanOptions{}); err == nil {
		t.Fatal("expected error")
	}
	start := 1
	if _, err := BuildPlan(students, PlanOptions{StartNIPD: &start}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildRenumberPlanOrderAndThreshold(t *testing.T) {
	students := []Student{
		{Nama: "A", Rombel: flex("X A"), NIPD: flex("100"), RegistrasiID: "a"},
		{Nama: "B", Rombel: flex("X B"), NIPD: flex("18000"), RegistrasiID: "b"},
		{Nama: "C", Rombel: flex("X B"), NIPD: flex("17978"), RegistrasiID: "c"},
		{Nama: "D", Rombel: flex("XI A"), NIPD: flex("17979"), RegistrasiID: "d"},
		{Nama: "E", Rombel: FlexString{}, NIPD: flex("18001"), RegistrasiID: "e"},
		{Nama: "F", Rombel: flex("X A"), NIPD: FlexString{}, RegistrasiID: "f"},
	}
	plan, err := BuildRenumberPlan(students, RenumberOptions{Threshold: 17978, StartNIPD: 17979})
	if err != nil {
		t.Fatal(err)
	}
	wantGroups := []string{"X A", "X B", "XI A", NoRombelLabel}
	if len(plan.Groups) != len(wantGroups) {
		t.Fatalf("groups = %d, want %d", len(plan.Groups), len(wantGroups))
	}
	for i, want := range wantGroups {
		if plan.Groups[i].Label != want {
			t.Fatalf("group[%d] = %q, want %q", i, plan.Groups[i].Label, want)
		}
	}
	if plan.Groups[0].Eligible != 0 || plan.Groups[1].Eligible != 1 || plan.Groups[2].Eligible != 1 || plan.Groups[3].Eligible != 1 {
		t.Fatalf("eligible per group = %+v", plan.Groups)
	}
	if plan.Eligible != 3 || plan.Kept != 3 || plan.Empty != 1 {
		t.Fatalf("eligible/kept/empty = %d/%d/%d", plan.Eligible, plan.Kept, plan.Empty)
	}
	if len(plan.Items) != 3 {
		t.Fatalf("items = %+v", plan.Items)
	}
	if plan.Items[0].RegistrasiID != "b" || plan.Items[0].NewNIPD != "17979" {
		t.Fatalf("item[0] = %+v", plan.Items[0])
	}
	if plan.Items[1].RegistrasiID != "d" || plan.Items[1].NewNIPD != "17980" {
		t.Fatalf("item[1] = %+v", plan.Items[1])
	}
	if plan.Items[2].RegistrasiID != "e" || plan.Items[2].NewNIPD != "17981" {
		t.Fatalf("item[2] = %+v", plan.Items[2])
	}
	if plan.OldMin != 17979 || plan.OldMax != 18001 || plan.NewMax != 17981 {
		t.Fatalf("ranges = old %d-%d new %d", plan.OldMin, plan.OldMax, plan.NewMax)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("conflicts = %v", plan.Conflicts)
	}
}

func TestBuildRenumberPlanDetectsConflictAndSelectedGroups(t *testing.T) {
	students := []Student{
		{Nama: "A", Rombel: flex("X A"), NIPD: flex("18000"), RegistrasiID: "a"},
		{Nama: "B", Rombel: flex("X B"), NIPD: flex("17979"), RegistrasiID: "b"},
	}
	plan, err := BuildRenumberPlan(students, RenumberOptions{
		Threshold: 17978, StartNIPD: 17979,
		SelectedGroups: map[string]bool{"X A": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].RegistrasiID != "a" {
		t.Fatalf("items = %+v", plan.Items)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0] != 17979 {
		t.Fatalf("conflicts = %v", plan.Conflicts)
	}
}

func TestBuildRenumberPlanOperators(t *testing.T) {
	students := []Student{
		{Nama: "A", Rombel: flex("X A"), NIPD: flex("99"), RegistrasiID: "a"},
		{Nama: "B", Rombel: flex("X A"), NIPD: flex("100"), RegistrasiID: "b"},
		{Nama: "C", Rombel: flex("X A"), NIPD: flex("101"), RegistrasiID: "c"},
		{Nama: "D", Rombel: flex("X A"), NIPD: flex("102"), RegistrasiID: "d"},
		{Nama: "E", Rombel: flex("X A"), NIPD: flex("abc"), RegistrasiID: "e"},
	}
	cases := []struct {
		op   string
		want int
	}{
		{">", 2},
		{">=", 3},
		{"<", 1},
		{"<=", 2},
	}
	for _, tc := range cases {
		plan, err := BuildRenumberPlan(students, RenumberOptions{
			Threshold: 100, Operator: tc.op, StartNIPD: 200,
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.op, err)
		}
		if plan.Operator != tc.op || plan.Eligible != tc.want {
			t.Fatalf("%s: operator=%q eligible=%d, want %d", tc.op, plan.Operator, plan.Eligible, tc.want)
		}
	}
	if _, err := BuildRenumberPlan(students, RenumberOptions{Threshold: 100, Operator: "=", StartNIPD: 200}); err == nil {
		t.Fatal("expected invalid operator error")
	}
}

func TestBuildRenumberPlanRejectsNegativeInput(t *testing.T) {
	students := []Student{{Nama: "A", NIPD: flex("18000"), RegistrasiID: "a"}}
	if _, err := BuildRenumberPlan(students, RenumberOptions{Threshold: -1, StartNIPD: 1}); err == nil {
		t.Fatal("expected negative threshold error")
	}
	if _, err := BuildRenumberPlan(students, RenumberOptions{Threshold: 1, StartNIPD: -1}); err == nil {
		t.Fatal("expected negative start error")
	}
}

func TestFingerprintChanges(t *testing.T) {
	a := []Student{{Nama: "A", NIPD: flex("1"), RegistrasiID: "a"}}
	b := []Student{{Nama: "A", NIPD: flex("2"), RegistrasiID: "a"}}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("fingerprint should change when NIPD changes")
	}
}

func TestDecodeStudentsPHPError(t *testing.T) {
	body := []byte("{ 'success' : false, 'message': 'Session telah habis. harap melakukan login kembali' }")
	_, _, err := decodeStudents(200, body)
	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("err = %v, want AuthError", err)
	}
}

func TestDecodeStudentsJSON(t *testing.T) {
	body := []byte(`{"success":true,"total":1,"rows":[{"nama":"Ani","nisn":"001","nipd":null,"rombel":"X A","registrasi_id":"r1","tingkat_pendidikan_id":"10"}]}`)
	rows, total, err := decodeStudents(200, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Nama != "Ani" || !rows[0].NIPD.IsEmpty() {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if total == nil || *total != 1 {
		t.Fatalf("total = %v", total)
	}
}

func TestFetchStudentsPaginationAndHeaders(t *testing.T) {
	all := []Student{
		{Nama: "A", NISN: flex("1"), RegistrasiID: "r1", PesertaDidikID: "p1"},
		{Nama: "B", NISN: flex("2"), RegistrasiID: "r2", PesertaDidikID: "p2"},
		{Nama: "C", NISN: flex("3"), RegistrasiID: "r3", PesertaDidikID: "p3"},
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Cookie") != "PHPSESSID=test" {
			t.Errorf("cookie not forwarded: %q", r.Header.Get("Cookie"))
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("token not forwarded: %q", r.Header.Get("Authorization"))
		}
		start := 0
		if v := r.URL.Query().Get("start"); v != "" {
			start, _ = strconv.Atoi(v)
		}
		end := start + 2
		if end > len(all) {
			end = len(all)
		}
		pageRows := all[start:end]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"total":   len(all),
			"rows":    pageRows,
		})
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, Cookie: "PHPSESSID=test", Token: "tok", Retries: 0})
	rows, err := client.FetchStudents(context.Background(), FetchOptions{
		SekolahID: "s1", NPSN: "123", Limit: 100,
		Progress: func(p FetchProgress) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestDecodeUpdatePHPStyleSuccess(t *testing.T) {
	body := []byte("{ 'success' : true, 'message' : 'Berhasil mengupdate RegistrasiPesertaDidik', 'rows' : {} }")
	if err := decodeUpdate(200, body); err != nil {
		t.Fatalf("PHP-style success rejected: %v", err)
	}
}

func TestUpdateNIPDPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/rest/RegistrasiPesertaDidik/reg-1") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("_dc") == "" {
			t.Error("missing _dc")
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		if payload["nipd"] != "100" || payload["registrasi_id"] != "reg-1" {
			t.Errorf("payload = %v", payload)
		}
		_, _ = w.Write([]byte(`{"success":true,"message":"Berhasil mengupdate RegistrasiPesertaDidik"}`))
	}))
	defer server.Close()

	client := New(Config{BaseURL: server.URL, Timeout: 5 * time.Second})
	if err := client.UpdateNIPD(context.Background(), "reg-1", "100"); err != nil {
		t.Fatal(err)
	}
}
