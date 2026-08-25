package models

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestGenericErrorCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       string
	}{
		{"mapped client status", http.StatusNotFound, ErrCodeNotFound},
		{"mapped server status", http.StatusBadGateway, ErrCodeBadGateway},
		{"unmapped 4xx falls back to class", http.StatusTeapot, ErrCodeClientError},
		{"unmapped 5xx falls back to class", http.StatusNotExtended, ErrCodeServerError},
		// Reaching the error path with a success status is a bug in the caller,
		// but the response still has to carry a code rather than an empty string.
		{"non-error status still yields a code", http.StatusOK, ErrCodeInternalError},
		{"zero value still yields a code", 0, ErrCodeInternalError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenericErrorCode(tt.statusCode); got != tt.want {
				t.Errorf("GenericErrorCode(%d) = %q, want %q", tt.statusCode, got, tt.want)
			}
		})
	}
}

// specPath is the repo-root openapi.yaml, relative to this package directory.
const specPath = "../../openapi.yaml"

// errorCodeEnumFromSpec extracts the values of the ErrorCode schema's enum.
//
// This scans rather than parsing YAML so the test adds no dependency for a
// drift guard. It fails loudly if the block cannot be located, so a spec
// reformat surfaces as a failing test rather than a silently empty comparison.
//
// The shape it expects, so a spec edit can be made deliberately rather than by
// trial and error against the regexes below:
//
//	components:
//	  schemas:
//	    ErrorCode:          <- exactly four spaces, then the schema name
//	      type: string
//	      enum:             <- any indent, the literal key on its own line
//	        - bad_request   <- one value per line, lowercase and underscores
//	        # comments and blank lines between values are fine
//
// Values stop being collected at the first line that is neither an item, a
// comment, nor blank. A sibling schema at four-space indent ends the block.
func errorCodeEnumFromSpec(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading %s: %v", specPath, err)
	}

	lines := strings.Split(string(raw), "\n")
	schema := -1
	for i, l := range lines {
		if strings.TrimRight(l, " ") == "    ErrorCode:" {
			schema = i
			break
		}
	}
	if schema == -1 {
		t.Fatalf("no ErrorCode schema found in %s — did the spec move or get renamed?", specPath)
	}

	enum := -1
	for i := schema + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		// a sibling schema at the same indent ends the block
		if regexp.MustCompile(`^    \S`).MatchString(lines[i]) {
			break
		}
		if trimmed == "enum:" {
			enum = i
			break
		}
	}
	if enum == -1 {
		t.Fatalf("ErrorCode schema in %s has no enum block", specPath)
	}

	item := regexp.MustCompile(`^\s+- ([a-z_]+)\s*$`)
	var values []string
	for i := enum + 1; i < len(lines); i++ {
		if m := item.FindStringSubmatch(lines[i]); m != nil {
			values = append(values, m[1])
			continue
		}
		if strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			continue // blank lines and the generic/specific section comments
		}
		break
	}
	if len(values) == 0 {
		t.Fatalf("ErrorCode enum in %s parsed as empty", specPath)
	}
	return values
}

// The Go constants and the published enum are exactly the pair that drifts —
// it's happened more than once already, each time as a fix for openapi.yaml
// falling out of step with the backend. This makes that drift a failing test
// instead of a client's problem.
func TestErrorCodeEnumMatchesSpec(t *testing.T) {
	inGo := make(map[string]bool, len(AllErrorCodes))
	for _, c := range AllErrorCodes {
		inGo[c] = true
	}

	inSpec := make(map[string]bool)
	for _, c := range errorCodeEnumFromSpec(t) {
		inSpec[c] = true
	}

	for c := range inGo {
		if !inSpec[c] {
			t.Errorf("code %q is in models.AllErrorCodes but missing from the ErrorCode enum in openapi.yaml", c)
		}
	}
	for c := range inSpec {
		if !inGo[c] {
			t.Errorf("code %q is in the ErrorCode enum in openapi.yaml but missing from models.AllErrorCodes", c)
		}
	}
}

// A status could otherwise be mapped to a code that was never published.
func TestGenericErrorCodesAreAllRegistered(t *testing.T) {
	registered := make(map[string]bool, len(AllErrorCodes))
	for _, c := range AllErrorCodes {
		registered[c] = true
	}
	for status, code := range genericErrorCodes {
		if !registered[code] {
			t.Errorf("status %d maps to %q, which is not in AllErrorCodes", status, code)
		}
	}
	for _, code := range []string{ErrCodeClientError, ErrCodeServerError, ErrCodeInternalError} {
		if !registered[code] {
			t.Errorf("fallback code %q is not in AllErrorCodes", code)
		}
	}
}

func TestAllErrorCodesHasNoDuplicates(t *testing.T) {
	seen := make(map[string]bool, len(AllErrorCodes))
	for _, c := range AllErrorCodes {
		if seen[c] {
			t.Errorf("duplicate entry %q in AllErrorCodes", c)
		}
		seen[c] = true
	}
}

// Every generic code must be distinct, or narrowing from one to a specific code
// later would be ambiguous for anyone reading telemetry.
func TestGenericErrorCodesAreUnique(t *testing.T) {
	seen := make(map[string]int, len(genericErrorCodes))
	for status, code := range genericErrorCodes {
		if prev, dup := seen[code]; dup {
			t.Errorf("code %q is mapped from both %d and %d", code, prev, status)
		}
		seen[code] = status
	}
}

// ErrorResponse.Code deliberately has no omitempty so a missing code shows up as
// "" rather than vanishing from the payload. That only helps if every producer
// goes through handlerutils.RespondWithError, which fills in a generic code for
// a blank one. A literal built anywhere else can ship `"code":""` — present in
// the body, absent from the published ErrorCode enum, and in breach of the
// spec's `required: [code]`.
//
// So the single construction site is the invariant, and this pins it.
func TestErrorResponseIsOnlyConstructedByTheResponseHelper(t *testing.T) {
	const allowed = "internal/handlers/handlerutils/httpresponse.go"

	root := repoRoot(t)

	literal := regexp.MustCompile(`\bmodels\.ErrorResponse\{|(?:^|[^.\w])ErrorResponse\{`)

	var offenders []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "ent", "vendor":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		scanned++
		if filepath.ToSlash(rel) == allowed {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for n, line := range strings.Split(string(body), "\n") {
			if literal.MatchString(line) {
				offenders = append(offenders, filepath.ToSlash(rel)+":"+strconv.Itoa(n+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}

	// The guard on the guard. A scan that reaches nothing reports no offenders
	// and passes, so this count is the only thing separating "clean" from
	// "never looked". The repo held 373 non-test .go files when this was written.
	if scanned < 200 {
		t.Fatalf("scanned only %d Go files under %s — the scan root is wrong, so this test is passing without checking anything",
			scanned, root)
	}

	if len(offenders) > 0 {
		t.Errorf("ErrorResponse built outside %s, which bypasses the blank-code fill:\n  %s\n"+
			"Route it through handlerutils.RespondWithError, or update this test with the reason it cannot be.",
			allowed, strings.Join(offenders, "\n  "))
	}
}

// repoRoot walks up from the working directory until it finds go.mod.
//
// This guard used to resolve the root as a fixed number of parent directories
// ("../.."), which is coupled to how deep this file happens to sit. Moving the
// package would have pointed the walk at the wrong directory — and a walk that
// finds no Go files reports no offenders, so the guard would have gone on
// passing while checking nothing.
//
// specPath above is deliberately left as a relative path: os.ReadFile fails
// loudly if it is wrong, which is the behavior this fix is trying to produce.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolving working directory: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("reached the filesystem root without finding go.mod")
		}
		dir = parent
	}
}
