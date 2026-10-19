package wmi

import "testing"

func TestCheckQuery(t *testing.T) {
	for _, ok := range []string{"SELECT * FROM X", "  select Name from X where A='b'", "ASSOCIATORS OF {X.Y='z'}", "References Of {a}"} {
		if err := CheckQuery(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"DELETE FROM X", "", "UPDATE X SET a=1", "SELECTION"} {
		if err := CheckQuery(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestCheckMethod(t *testing.T) {
	if err := CheckMethod("GetSummaryInformation"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"RequestStateChange", "DefineSystem", "", "getsummaryinformation"} {
		if err := CheckMethod(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestRowAccessors(t *testing.T) {
	r := Row{
		"S": "text", "U64": "18446744073709551615", "I32": int32(-5), "U16": uint16(7), "B": true,
		"Arr": []any{"a", nil, int32(2)}, "Nums": []any{"1", int32(2), uint64(3)}, "F": float64(1.5), "N": nil,
	}
	if r.String("S") != "text" || r.String("N") != "" || r.String("missing") != "" || r.String("I32") != "-5" {
		t.Error("String")
	}
	if r.Int("U64") != -1 || r.Int("I32") != -5 || r.Int("U16") != 7 || r.Int("S") != 0 {
		t.Errorf("Int: %d %d %d", r.Int("U64"), r.Int("I32"), r.Int("U16"))
	}
	if got := r.Strings("Arr"); len(got) != 2 || got[0] != "a" || got[1] != "2" {
		t.Errorf("Strings = %v", got)
	}
	if got := r.Ints("Nums"); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("Ints = %v", got)
	}
	if !r.Bool("B") || r.Bool("N") || !r.Has("S") || r.Has("N") || r.Float("F") != 1.5 {
		t.Error("Bool/Has/Float")
	}
	if Escape(`C:\x 'q'`) != `C:\\x \'q\'` {
		t.Error("Escape")
	}
}
