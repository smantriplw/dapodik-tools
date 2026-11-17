package app

import "testing"

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"=SUM(A1:A2)": "'=SUM(A1:A2)",
		"+62812":      "'+62812",
		"-123":        "'-123",
		"@cmd":        "'@cmd",
		"Ani":         "Ani",
		"":            "",
	}
	for input, want := range cases {
		if got := csvSafe(input); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", input, got, want)
		}
	}
}
