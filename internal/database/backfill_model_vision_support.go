package database

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/ent/model"
	appmodels "github.com/theimaginaryfoundation/what-iff/internal/models"

	"go.uber.org/zap"
)

// backfillModelVisionSupport sets vision_support on rows created before the column
// existed (NULL), using the provider/name heuristics that gated image input before
// it became a per-model field. Rows with a value are never touched, so admin edits stick.
func backfillModelVisionSupport(ctx context.Context, client *ent.Client, logger *zap.Logger) error {
	rows, err := client.Model.Query().Where(model.VisionSupportIsNil()).All(ctx)
	if err != nil {
		return err
	}
	for _, m := range rows {
		vision := legacyVisionSupport(string(m.Provider), m.Name)
		if err := client.Model.UpdateOneID(m.ID).SetVisionSupport(vision).Exec(ctx); err != nil {
			return err
		}
		logger.Info("Backfilled model vision_support",
			zap.String("name", m.Name),
			zap.String("provider", string(m.Provider)),
			zap.Bool("vision_support", vision),
		)
	}
	return nil
}

// legacyVisionSupport reproduces the pre-column routing: OpenAI, Anthropic, z.ai and
// Google always received images; Qwen, Mistral and MiMo by model-id heuristics;
// DeepSeek never.
func legacyVisionSupport(provider, name string) bool {
	switch appmodels.ProviderForModel(provider, name) {
	case appmodels.ModelProviderOpenAI, appmodels.ModelProviderAnthropic,
		appmodels.ModelProviderZAI, appmodels.ModelProviderGoogle:
		return true
	case appmodels.ModelProviderQwen:
		return legacyQwenVision(name)
	case appmodels.ModelProviderMistral:
		return legacyMistralVision(name)
	case appmodels.ModelProviderXiaomi:
		return legacyXiaomiVision(name)
	default:
		return false
	}
}

// mimoVersionPattern captures the major/minor version from MiMo ids such as
// "mimo-v2.6", "mimo-v2.6-pro" or "MiMo-2.6". The trailing guard keeps
// parameter-size ids like "mimo-7b-rl" from being read as version 7.
var mimoVersionPattern = regexp.MustCompile(`mimo-v?(\d+)(?:\.(\d+))?(?:[^0-9a-z.]|$)`)

func legacyXiaomiVision(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if strings.Contains(n, "omni") || strings.Contains(n, "-vl") {
		return true
	}
	m := mimoVersionPattern.FindStringSubmatch(n)
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor := 0
	if m[2] != "" {
		minor, _ = strconv.Atoi(m[2])
	}
	return major > 2 || (major == 2 && minor >= 6)
}

func legacyQwenVision(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(n, "qwen3.7") || strings.Contains(n, "qwen3.6") {
		return true
	}
	if strings.HasPrefix(n, "qwen3.5-plus") || strings.HasPrefix(n, "qwen3.5-flash") {
		return true
	}
	return strings.Contains(n, "qwen-vl") || strings.Contains(n, "qwen3-vl") || strings.Contains(n, "qwen2.5-vl")
}

func legacyMistralVision(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, marker := range []string{"mistral-medium", "mistral-large", "mistral-small", "ministral", "magistral"} {
		if strings.Contains(n, marker) {
			return true
		}
	}
	return false
}
