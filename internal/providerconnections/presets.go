package providerconnections

import (
	"strings"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Flavor is the API a connection speaks.
type Flavor string

const (
	FlavorOpenAI    Flavor = "openai"
	FlavorAnthropic Flavor = "anthropic"
)

// Valid reports whether f is a flavor a custom connection may choose.
func (f Flavor) Valid() bool { return f == FlavorOpenAI || f == FlavorAnthropic }

// Preset ids beyond the built-in providers, whose ids are models.ModelProvider
// values.
const (
	PresetOpenRouter = "openrouter"
	PresetOllama     = "ollama"
	PresetLMStudio   = "lmstudio"
	PresetCustom     = "custom"
)

// Preset fills in a connection's details. It is what the "Add provider" form
// offers.
type Preset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Flavor is empty for Custom, where the account chooses it.
	Flavor Flavor `json:"flavor,omitempty"`
	// BaseURL is the address a new connection gets; empty for Custom.
	BaseURL string `json:"base_url,omitempty"`
	// AddressEditable is true where the account may change the address: a
	// server on this computer, or Custom.
	AddressEditable bool `json:"address_editable"`
	// KeyRequired is false for servers that usually run without a key.
	KeyRequired bool `json:"key_required"`
	// Builtin is true for the providers the app has always routed to. An
	// account has at most one connection to each (ADR 0x027).
	Builtin bool `json:"builtin"`
	// Local is true for servers expected on the same computer.
	Local bool `json:"local"`
	// KeysURL is where to create a key, when known.
	KeysURL string `json:"keys_url,omitempty"`

	// How this provider differs from the plain flavor when a connection is
	// checked; see Target. Not part of the API.
	listFlavor   Flavor
	listURL      string
	keyCheckURL  string
	badKeyStatus int
	// ollamaDetails asks Ollama's native API about each model after listing:
	// its OpenAI-compatible list reports only ids, while /api/show reports the
	// model's capabilities.
	ollamaDetails bool
}

// Where a provider's model list does not check the key, or reports a bad key
// unusually. Found by listing each provider with a key it does not know.
const (
	// z.ai's Anthropic-compatible model list, at its default address, answers
	// without checking the key; its OpenAI-compatible one checks it. Only that
	// default is redirected: a deployment that points z.ai elsewhere is listed
	// where it pointed it, so the key never goes to a host it did not configure.
	zaiDefaultURL = "https://api.z.ai/api/anthropic"
	zaiListURL    = "https://api.z.ai/api/paas/v4"
	// OpenRouter's model list is public; this endpoint checks the key.
	openRouterKeyCheckURL = "https://openrouter.ai/api/v1/key"
)

// Target is a check of one connection: list its provider's models with its key.
type Target struct {
	Flavor  Flavor
	BaseURL string
	Key     string
	// KeyCheckURL, when set, is fetched with the key after a successful listing,
	// for a provider whose model list is public.
	KeyCheckURL string
	// BadKeyStatus, when set, is a status the provider answers a bad key with
	// besides 401 and 403.
	BadKeyStatus int
	// OllamaDetails, when set, reads each model's capabilities from Ollama's
	// native /api/show after listing.
	OllamaDetails bool
}

// target is the check for a connection made from p, speaking flavor at
// address with key.
func (p Preset) target(flavor Flavor, address, key string) Target {
	t := Target{Flavor: flavor, BaseURL: address, Key: key, KeyCheckURL: p.keyCheckURL, BadKeyStatus: p.badKeyStatus, OllamaDetails: p.ollamaDetails}
	if p.listURL != "" {
		t.Flavor, t.BaseURL = p.listFlavor, p.listURL
	}
	return t
}

// builtinFlavors is the API each built-in provider's model list is read from.
// Google, Mistral, DeepSeek, Qwen and Xiaomi are reached through their
// OpenAI-compatible endpoints; z.ai through its Anthropic-compatible one.
var builtinFlavors = map[models.ModelProvider]Flavor{
	models.ModelProviderOpenAI:    FlavorOpenAI,
	models.ModelProviderAnthropic: FlavorAnthropic,
	models.ModelProviderZAI:       FlavorAnthropic,
	models.ModelProviderGoogle:    FlavorOpenAI,
	models.ModelProviderMistral:   FlavorOpenAI,
	models.ModelProviderDeepSeek:  FlavorOpenAI,
	models.ModelProviderQwen:      FlavorOpenAI,
	models.ModelProviderXiaomi:    FlavorOpenAI,
}

// Presets lists every preset in the order the form offers them.
//
// builtinURLs is the address the deployment reaches each built-in provider at,
// the same one its client is built with. localHost is the name a server on
// this computer is reached by: "localhost", or "host.docker.internal" when the
// API runs in a container, where localhost is the container itself.
func Presets(builtinURLs map[models.ModelProvider]string, localHost string) []Preset {
	out := make([]Preset, 0, len(models.ModelProviders)+4)
	for _, p := range models.ModelProviders {
		preset := Preset{
			ID:          string(p),
			Name:        p.DisplayName(),
			Flavor:      builtinFlavors[p],
			BaseURL:     builtinURLs[p],
			KeyRequired: true,
			Builtin:     true,
		}
		switch p {
		case models.ModelProviderZAI:
			if strings.TrimRight(preset.BaseURL, "/") == zaiDefaultURL {
				preset.listFlavor, preset.listURL = FlavorOpenAI, zaiListURL
			}
		case models.ModelProviderOpenAI:
			preset.KeysURL = "https://platform.openai.com/api-keys"
		case models.ModelProviderGoogle:
			// Google answers a key it does not know with 400, "API key not valid".
			preset.badKeyStatus = 400
		}
		out = append(out, preset)
	}
	return append(out,
		Preset{
			ID:          PresetOpenRouter,
			Name:        "OpenRouter",
			Flavor:      FlavorOpenAI,
			BaseURL:     "https://openrouter.ai/api/v1",
			KeyRequired: true,
			KeysURL:     "https://openrouter.ai/keys",
			keyCheckURL: openRouterKeyCheckURL,
		},
		Preset{
			ID:              PresetOllama,
			Name:            "Ollama",
			Flavor:          FlavorOpenAI,
			BaseURL:         "http://" + localHost + ":11434/v1",
			AddressEditable: true,
			Local:           true,
			ollamaDetails:   true,
		},
		Preset{
			ID:              PresetLMStudio,
			Name:            "LM Studio",
			Flavor:          FlavorOpenAI,
			BaseURL:         "http://" + localHost + ":1234/v1",
			AddressEditable: true,
			Local:           true,
		},
		Preset{
			ID:              PresetCustom,
			Name:            "Custom",
			AddressEditable: true,
		},
	)
}

// findPreset returns the preset with id, or false.
func findPreset(presets []Preset, id string) (Preset, bool) {
	for _, p := range presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
