package agent

import (
	"os"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3/responses"
)

// OpenAI service tiers are organization-level entitlements, not universal
// options. "priority" requires a paid commitment, and "flex" is limited to
// particular accounts and models. Requesting one the organization behind the
// key does not have turns an otherwise valid request into an error, which for
// a self-hoster looks like the application being broken rather than like an
// option they never asked for.
//
// So they are opt-in. Unset, the field is omitted (it is `omitzero` in the
// SDK) and OpenAI applies the account's own default — the correct behaviour
// for anyone running this with their own key. A deployment whose organization
// does have the entitlement sets OPENAI_SERVICE_TIERS=true to get the latency
// and cost characteristics the tiers were chosen for.
const serviceTiersEnvVar = "OPENAI_SERVICE_TIERS"

var (
	serviceTiersOnce    sync.Once
	serviceTiersEnabled bool
)

// onceReset exists so tests can re-read the environment; production reads it
// exactly once.
func onceReset() sync.Once { return sync.Once{} }

func serviceTiersAllowed() bool {
	serviceTiersOnce.Do(func() {
		serviceTiersEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv(serviceTiersEnvVar)), "true")
	})
	return serviceTiersEnabled
}

// openAIServiceTier returns preferred when service tiers are enabled, and the
// zero value otherwise, which the SDK omits from the request.
func openAIServiceTier(preferred responses.ResponseNewParamsServiceTier) responses.ResponseNewParamsServiceTier {
	if !serviceTiersAllowed() {
		return ""
	}
	return preferred
}
