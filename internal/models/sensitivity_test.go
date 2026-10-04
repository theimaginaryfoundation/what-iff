package models

import (
	"reflect"
	"testing"
)

func TestMemorySensitivityOrdering(t *testing.T) {
	if !(MemorySensitivityPublic.Rank() < MemorySensitivityPersonal.Rank() && MemorySensitivityPersonal.Rank() < MemorySensitivitySensitive.Rank()) {
		t.Fatal("expected public < personal < sensitive")
	}
	cases := []struct {
		level, limit MemorySensitivity
		want         bool
	}{
		{MemorySensitivityPublic, MemorySensitivityPublic, true},
		{MemorySensitivityPersonal, MemorySensitivityPublic, false},
		{MemorySensitivitySensitive, MemorySensitivityPublic, false},
		{MemorySensitivityPersonal, MemorySensitivityPersonal, true},
		{MemorySensitivitySensitive, MemorySensitivityPersonal, false},
		{MemorySensitivitySensitive, MemorySensitivitySensitive, true},
		{"", MemorySensitivitySensitive, true},      // legacy row reads as personal
		{"", MemorySensitivityPublic, false},        // and is not public
		{MemorySensitivitySensitive, "", true},      // an empty limit is unrestricted
		{"bogus", MemorySensitivityPersonal, true},  // unknown level reads as personal
		{MemorySensitivitySensitive, "bogus", true}, // unknown limit is unrestricted
	}
	for _, c := range cases {
		if got := c.level.AllowedUnder(c.limit); got != c.want {
			t.Errorf("%q under %q = %v, want %v", c.level, c.limit, got, c.want)
		}
	}
}

func TestMemorySensitivityHelpers(t *testing.T) {
	if MemorySensitivitySensitive.Restricted() || MemorySensitivity("").Restricted() {
		t.Error("sensitive/empty limit must be unrestricted")
	}
	if !MemorySensitivityPersonal.Restricted() || !MemorySensitivityPublic.Restricted() {
		t.Error("personal/public limit must be restricted")
	}
	if got := MostRestricted(MemorySensitivityPublic, MemorySensitivitySensitive); got != MemorySensitivitySensitive {
		t.Errorf("MostRestricted = %q", got)
	}
	if got := MostRestricted("", MemorySensitivityPublic); got != MemorySensitivityPersonal {
		t.Errorf("empty counts as personal, got %q", got)
	}
	capCases := []struct{ want, limit, out MemorySensitivity }{
		{"", MemorySensitivitySensitive, MemorySensitivityPersonal},
		{"", MemorySensitivityPublic, MemorySensitivityPublic},
		{MemorySensitivitySensitive, MemorySensitivityPersonal, MemorySensitivityPersonal},
		{MemorySensitivityPublic, MemorySensitivitySensitive, MemorySensitivityPublic},
		{MemorySensitivitySensitive, MemorySensitivitySensitive, MemorySensitivitySensitive},
	}
	for _, c := range capCases {
		if got := CapToLimit(c.want, c.limit); got != c.out {
			t.Errorf("CapToLimit(%q,%q) = %q, want %q", c.want, c.limit, got, c.out)
		}
	}
	if got := SensitivitiesUpTo(MemorySensitivityPersonal); !reflect.DeepEqual(got, []string{"public", "personal"}) {
		t.Errorf("SensitivitiesUpTo(personal) = %v", got)
	}
	if got := SensitivitiesUpTo(""); len(got) != 3 {
		t.Errorf("empty limit lists all levels, got %v", got)
	}
	if s, ok := ParseMemorySensitivity(" Sensitive "); !ok || s != MemorySensitivitySensitive {
		t.Errorf("ParseMemorySensitivity = %q %v", s, ok)
	}
	if _, ok := ParseMemorySensitivity("secret"); ok {
		t.Error("unknown value must not parse")
	}
	var nilChat *Chat
	if nilChat.MemoryRestricted() || (&Chat{}).MemoryRestricted() {
		t.Error("nil/empty chat is unrestricted")
	}
	if !(&Chat{MemorySensitivityLimit: MemorySensitivityPublic}).MemoryRestricted() {
		t.Error("public-limited chat is restricted")
	}
}
