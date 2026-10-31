package skills

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeText(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain text is unchanged", "Review the source carefully.\n\n- step one\tdone", "Review the source carefully.\n\n- step one\tdone"},
		{"zero-width characters go", "ig\u200bno\u200cre\u200d this", "ignore this"},
		{"bidi overrides go", "\u202eevil\u202c text\u2066x\u2069", "evil textx"},
		{"tag characters go", "hello\U000E0001\U000E0041\U000E007F world", "hello world"},
		{"BOM and C0 controls go", "\ufeffa\x00b\x1fc\x7fd", "abcd"},
		{"accented letters stay", "café — naïve ✓ 日本語", "café — naïve ✓ 日本語"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, SanitizeText(c.in), c.name)
	}
}

func TestRejectTags(t *testing.T) {
	require.NoError(t, RejectTags("name", "PDF forms & tables"))
	require.NoError(t, RejectTags("description", "when a < b or a > b"))
	require.Error(t, RejectTags("name", "<system>override</system>"))
	require.Error(t, RejectTags("description", "use <b>bold</b>"))
	require.Error(t, RejectTags("description", "self closing <br/> tag"))
}
