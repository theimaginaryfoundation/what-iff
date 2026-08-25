package provider

import (
	"strings"
	"unicode/utf8"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/tiktoken-go/tokenizer"
)

const SHORT_THRESHOLD = 16

type TokenCounter struct {
	enc tokenizer.Codec
}

func NewTokenCounter() *TokenCounter {
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		// Tokenization is a best-effort feature; fall back to a heuristic counter rather than
		// failing initialization and risking panics downstream.
		return &TokenCounter{enc: nil}
	}
	return &TokenCounter{enc: enc}
}
func (c *TokenCounter) CountTokens(text string) (int, error) {
	if c == nil || c.enc == nil {
		return heuristicTokenCount(text), nil
	}
	return c.enc.Count(text)
}

// heuristicTokenCount provides a conservative-ish approximation when tokenizer init is unavailable.
// Rough heuristic: ~4 chars per token for English-ish text.
func heuristicTokenCount(text string) int {
	if text == "" {
		return 0
	}
	charCount := utf8.RuneCountInString(text)
	// ceil(charCount / 4)
	return (charCount + 3) / 4
}

// selectCarryOverTurns expects messages ordered by descending SentAt (newest first).
// It returns up to maxTurns turns ordered oldest->newest; each turn is {user, assistant}.
//
// Token behavior:
// - Total tokens across returned turns is <= maxTokens.
// - If the most recent turn alone exceeds maxTokens, it is truncated to fit maxTokens.
func (c *TokenCounter) SelectCarryOverTurns(recent []*models.ChatMessage, maxTurns, maxTokens int) [][2]*models.ChatMessage {
	if maxTurns <= 0 || maxTokens <= 0 {
		return nil
	}

	remainingTokens := maxTokens
	var turnsNewestFirst [][2]*models.ChatMessage

	for i := 0; i < len(recent) && len(turnsNewestFirst) < maxTurns; i++ {
		aMsg := recent[i]
		if aMsg == nil || aMsg.Origin != models.MessageOriginAssistant {
			continue
		}

		// Find preceding user message (older than assistant => later in the slice).
		var uMsg *models.ChatMessage
		for j := i + 1; j < len(recent); j++ {
			if recent[j] != nil && recent[j].Origin == models.MessageOriginUser {
				uMsg = recent[j]
				i = j // advance outer loop past the user we consumed
				break
			}
		}
		if uMsg == nil {
			break
		}

		uTokens, aTokens := 0, 0
		if n, err := c.CountTokens(uMsg.Message); err == nil {
			uTokens = n
		}
		if n, err := c.CountTokens(aMsg.Message); err == nil {
			aTokens = n
		}

		turnTokens := uTokens + aTokens

		// If this turn won't fit, truncate it (but only if we have budget for *some* content).
		if turnTokens > remainingTokens {
			if remainingTokens < 20 {
				break
			}
			uTrim, aTrim := c.truncateTurnToTokenBudget(uMsg.Message, aMsg.Message, uTokens, aTokens, remainingTokens)
			uMsg = &models.ChatMessage{Message: uTrim, Origin: models.MessageOriginUser}
			aMsg = &models.ChatMessage{Message: aTrim, Origin: models.MessageOriginAssistant}
			turnsNewestFirst = append(turnsNewestFirst, [2]*models.ChatMessage{uMsg, aMsg})
			break
		}

		remainingTokens -= turnTokens
		turnsNewestFirst = append(turnsNewestFirst, [2]*models.ChatMessage{uMsg, aMsg})
		if remainingTokens <= 0 {
			break
		}
	}

	// Reverse to oldest->newest for prompt order.
	for l, r := 0, len(turnsNewestFirst)-1; l < r; l, r = l+1, r-1 {
		turnsNewestFirst[l], turnsNewestFirst[r] = turnsNewestFirst[r], turnsNewestFirst[l]
	}
	return turnsNewestFirst
}
func (c *TokenCounter) truncateTurnToTokenBudget(userText, assistantText string, userTokens, assistantTokens, budget int) (string, string) {
	if budget <= 0 {
		return "", ""
	}

	// Allocate budget proportionally by original token sizes, but keep a minimum for each side.
	const minSideBudget = 25
	uBudget, aBudget := budget/2, budget-budget/2
	total := userTokens + assistantTokens
	if total > 0 {
		uBudget = (budget * userTokens) / total
		aBudget = budget - uBudget
	}
	if uBudget < minSideBudget {
		uBudget = minSideBudget
	}
	if aBudget < minSideBudget {
		aBudget = minSideBudget
	}
	if uBudget+aBudget > budget {
		// Reduce assistant first.
		over := (uBudget + aBudget) - budget
		if aBudget > over {
			aBudget -= over
		} else {
			aBudget = 1
		}
	}

	return c.truncateToTokenBudget(userText, uBudget), c.truncateToTokenBudget(assistantText, aBudget)
}

func (c *TokenCounter) truncateToTokenBudget(text string, budget int) string {
	text = strings.TrimSpace(text)
	if budget <= 0 || text == "" {
		return ""
	}
	if n, err := c.CountTokens(text); err == nil && n <= budget {
		return text
	}

	// Binary search the largest prefix that fits in the budget.
	// We avoid token-level slicing by using substring prefixes and the counter as an oracle.
	const suffix = "…"
	runes := []rune(text)
	lo, hi := 0, len(runes)
	best := 0
	for lo <= hi {
		mid := (lo + hi) / 2
		candidate := strings.TrimSpace(string(runes[:mid]))
		if candidate == "" {
			lo = mid + 1
			continue
		}
		if candidate != text {
			candidate = candidate + suffix
		}
		n, err := c.CountTokens(candidate)
		if err == nil && n <= budget {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best <= 0 {
		// If we can't fit anything meaningful, fall back to a short prefix.
		if len(runes) > SHORT_THRESHOLD {
			return strings.TrimSpace(string(runes[:SHORT_THRESHOLD])) + suffix
		}
		return text
	}
	candidate := strings.TrimSpace(string(runes[:best]))
	if candidate == "" {
		return ""
	}
	if best < len(runes) {
		candidate += suffix
	}
	return candidate
}
