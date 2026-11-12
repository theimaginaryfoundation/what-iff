package officecomments

import "testing"

func TestPeopleRoundTrip(t *testing.T) {
	for _, id := range []string{"user:u-1", "agent:p-1"} {
		if got := PersonID(WhatIffProvider, ProviderUserID(id), "Pat"); got != id {
			t.Fatalf("%s came back as %s", id, got)
		}
	}
	if ProviderUserID("author:Pat") != "" || PersonID("AD", "x", "Pat") != "author:Pat" {
		t.Fatal("someone the file only names")
	}
}
