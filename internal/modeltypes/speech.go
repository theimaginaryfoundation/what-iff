package modeltypes

// SpeechVoice is one base voice a speech model offers (ADR 0x052 §2): the id the
// provider takes, and the name people see.
type SpeechVoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
