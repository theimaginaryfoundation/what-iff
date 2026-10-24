package models

// Experimental features: work that ships switched off and is turned on deliberately. Each has an
// operator switch of its own (an environment variable read by the feature). A build that links a
// per-user experimental gate (featuregate.NewExperimental) also requires the feature on the user
// (User.experimental_features, set by an admin); without one, the operator switch decides for
// everyone.

// ExperimentalFeatureBuzzRelay is the Buzz relay (docs/adr/0x028-buzz-relay.md), switched on for
// the server by BUZZ_RELAY_ENABLED.
const ExperimentalFeatureBuzzRelay = "buzz_relay"

// ExperimentalFeature describes one experimental feature, for the admin's toggles.
type ExperimentalFeature struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ExperimentalFeatures lists every experimental feature that can be turned on for a user.
var ExperimentalFeatures = []ExperimentalFeature{
	{
		Name:        ExperimentalFeatureBuzzRelay,
		Label:       "Buzz relay",
		Description: "Connect personas to Buzz communities, where people and agents share channels.",
	},
}

// IsExperimentalFeature reports whether name is a known experimental feature.
func IsExperimentalFeature(name string) bool {
	for _, f := range ExperimentalFeatures {
		if f.Name == name {
			return true
		}
	}
	return false
}
