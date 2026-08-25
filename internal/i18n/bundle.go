// Package i18n provides application-wide internationalisation support.
//
// Two English translation files are loaded from internal/i18n/assets and
// embedded into the binary at compile time:
//   - internal/i18n/assets/en.toml          — customer-facing strings (API responses, UI messages)
//   - internal/i18n/assets/en-internal.toml — internal logging strings (structured log messages)
//
// Usage:
//
//	i18n.T("chat.not_found")                          // returns "chat not found"
//	i18n.T1("chat.not_found_or_unauthorized", "ChatID", id, "UserID", uid)
package i18n

import (
	"fmt"

	"github.com/BurntSushi/toml"
	translations "github.com/theimaginaryfoundation/what-iff/internal/i18n/assets"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var localizer *goi18n.Localizer

func init() {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// Both files are loaded under the "en.toml" virtual name so go-i18n
	// assigns them to the English locale. Messages are merged into a single
	// localizer; duplicate keys across files will panic at startup.
	for _, f := range []struct {
		data []byte
		name string
	}{
		{translations.EnData, "en.toml"},
		{translations.EnInternalData, "en-internal.toml"},
	} {
		if _, err := bundle.ParseMessageFileBytes(f.data, f.name); err != nil {
			panic(fmt.Sprintf("i18n: failed to parse %s: %v", f.name, err))
		}
	}

	localizer = goi18n.NewLocalizer(bundle, language.English.String())
}

// T returns the translated string for the given message ID using the default
// English locale. Falls back to the ID itself when the message is not found,
// so callers never receive an empty string.
func T(id string) string {
	msg, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: id})
	if err != nil {
		return id
	}
	return msg
}

// TWith returns the translated string for the given message ID with template data
// applied. Falls back to the ID itself when the message is not found.
func TWith(id string, data interface{}) string {
	msg, err := localizer.Localize(&goi18n.LocalizeConfig{
		MessageID:    id,
		TemplateData: data,
	})
	if err != nil {
		return id
	}
	return msg
}

// T1 through T5 are lightweight wrappers around TWith for the common case of
// passing a small number of scalar values (IDs, counts, short strings) as
// template data. Each call allocates a map, so avoid using them in tight loops
// or with large structs/slices — use TWith with a pre-built map instead.

// T1 translates id with one named template variable.
func T1(id, key1 string, value1 interface{}) string {
	return TWith(id, map[string]interface{}{key1: value1})
}

// T2 translates id with two named template variables.
func T2(id, key1 string, value1 interface{}, key2 string, value2 interface{}) string {
	return TWith(id, map[string]interface{}{key1: value1, key2: value2})
}

// T3 translates id with three named template variables.
func T3(id, key1 string, value1 interface{}, key2 string, value2 interface{}, key3 string, value3 interface{}) string {
	return TWith(id, map[string]interface{}{key1: value1, key2: value2, key3: value3})
}

// T4 translates id with four named template variables.
func T4(id, key1 string, value1 interface{}, key2 string, value2 interface{}, key3 string, value3 interface{}, key4 string, value4 interface{}) string {
	return TWith(id, map[string]interface{}{key1: value1, key2: value2, key3: value3, key4: value4})
}

// T5 translates id with five named template variables.
func T5(id, key1 string, value1 interface{}, key2 string, value2 interface{}, key3 string, value3 interface{}, key4 string, value4 interface{}, key5 string, value5 interface{}) string {
	return TWith(id, map[string]interface{}{key1: value1, key2: value2, key3: value3, key4: value4, key5: value5})
}
