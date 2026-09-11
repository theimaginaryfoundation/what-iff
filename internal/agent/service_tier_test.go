package agent

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

func resetServiceTierCache() {
	serviceTiersOnce = onceReset()
}

// The default must omit the field. A self-hoster's organization generally has
// neither flex nor priority, and asking for one turns a valid request into an
// error that reads like the app is broken.
func TestServiceTierOmittedByDefault(t *testing.T) {
	resetServiceTierCache()
	t.Setenv(serviceTiersEnvVar, "")
	if got := openAIServiceTier(responses.ResponseNewParamsServiceTierFlex); got != "" {
		t.Fatalf("want omitted, got %q", got)
	}
	resetServiceTierCache()
	t.Setenv(serviceTiersEnvVar, "")
	if got := openAIServiceTier(responses.ResponseNewParamsServiceTierPriority); got != "" {
		t.Fatalf("want omitted, got %q", got)
	}
}

// A deployment whose organization does have the entitlement opts in and gets
// the tier the call site asked for.
func TestServiceTierHonouredWhenEnabled(t *testing.T) {
	resetServiceTierCache()
	t.Setenv(serviceTiersEnvVar, "true")
	if got := openAIServiceTier(responses.ResponseNewParamsServiceTierFlex); got != responses.ResponseNewParamsServiceTierFlex {
		t.Fatalf("want flex, got %q", got)
	}
}

func TestServiceTierIgnoresOtherValues(t *testing.T) {
	resetServiceTierCache()
	t.Setenv(serviceTiersEnvVar, "yes")
	if got := openAIServiceTier(responses.ResponseNewParamsServiceTierFlex); got != "" {
		t.Fatalf("only \"true\" enables tiers; got %q", got)
	}
}
