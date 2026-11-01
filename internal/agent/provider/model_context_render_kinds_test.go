package provider

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notRendered are the kinds that are deliberately not part of the rendered input.
var notRendered = map[ModelContextSegmentKind]string{
	SegmentKindToolDefinitions: "tool schemas are sent as request tools, outside the input",
}

// declaredSegmentKinds reads every ModelContextSegmentKind constant from model_context.go, so a
// kind added there is covered here without anyone remembering to list it.
func declaredSegmentKinds(t *testing.T) []ModelContextSegmentKind {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "model_context.go", nil, 0)
	require.NoError(t, err)
	var kinds []ModelContextSegmentKind
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if ident, ok := vs.Type.(*ast.Ident); !ok || ident.Name != "ModelContextSegmentKind" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				require.True(t, ok, "segment kinds are string literals")
				s, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				kinds = append(kinds, ModelContextSegmentKind(s))
			}
		}
	}
	require.NotEmpty(t, kinds)
	return kinds
}

// Every segment kind must reach the model on every provider. A renderer switch without a case
// for a kind drops it silently, which is how the skills and skills-index segments once went
// missing while the Context X-ray still counted them.
func TestRenderers_EmitEverySegmentKind(t *testing.T) {
	t.Parallel()
	renderers := map[string]func(*ModelContext) any{
		"claude": func(c *ModelContext) any {
			system, messages := renderClaudeContext(c)
			return []any{system, messages}
		},
		"openai": func(c *ModelContext) any { return RenderOpenAIInputItems(c) },
		"gemini": func(c *ModelContext) any { return renderGeminiMessages(c) },
	}
	for _, kind := range declaredSegmentKinds(t) {
		if _, skip := notRendered[kind]; skip {
			continue
		}
		marker := "marker-for-" + string(kind)
		for name, render := range renderers {
			if name == "openai" && kind == SegmentKindSystemPrompt {
				continue // the caller sends it as ResponseNewParams.Instructions
			}
			ctx := &ModelContext{}
			ctx.Append(kind, RoleUser, marker, false)
			out, err := json.Marshal(render(ctx))
			require.NoError(t, err)
			assert.Contains(t, string(out), marker, "%s renderer drops segment kind %q", name, kind)
		}
	}
}
