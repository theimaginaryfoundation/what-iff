package database

import (
	"strings"
	"testing"
)

func TestSubscriptionTierCheckWithUltra_EntArrayShape(t *testing.T) {
	t.Parallel()

	def := `((subscription_tier)::text = ANY ((ARRAY['low'::character varying, 'medium'::character varying, 'high'::character varying])::text[]))`
	got, ok := subscriptionTierCheckWithUltra(def)
	if !ok {
		t.Fatal("expected ent ARRAY check shape to match")
	}
	if !strings.Contains(got, "'ultra'::character varying") {
		t.Fatalf("expected ultra in ARRAY check, got %q", got)
	}

	withUltra := `((subscription_tier)::text = ANY ((ARRAY['low'::character varying, 'medium'::character varying, 'high'::character varying, 'ultra'::character varying])::text[]))`
	got, ok = subscriptionTierCheckWithUltra(withUltra)
	if !ok {
		t.Fatal("expected ultra-inclusive ent check to match")
	}
	if got != withUltra {
		t.Fatalf("expected idempotent result, got %q", got)
	}
}

func TestSubscriptionTierCheckWithUltra_InListShape(t *testing.T) {
	t.Parallel()

	def := `subscription_tier IN ('low', 'medium', 'high')`
	got, ok := subscriptionTierCheckWithUltra(def)
	if !ok {
		t.Fatal("expected IN-list shape to match")
	}
	if got != `subscription_tier IN ('low', 'medium', 'high', 'ultra')` {
		t.Fatalf("got %q", got)
	}
}
