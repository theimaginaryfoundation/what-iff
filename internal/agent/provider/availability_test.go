package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/responses"
)

// Provider methods dereference their client, so calling one on a provider that
// was never constructed used to panic — and before clients were built
// unconditionally, a nil-provider guard in the agent is what stopped that.
// Those guards are gone, so the providers themselves have to degrade to an
// error. A panic here would take down the job worker that called it.
func TestNilProviderReturnsErrorRatherThanPanicking(t *testing.T) {
	ctx := context.Background()

	t.Run("openai call", func(t *testing.T) {
		var p *OpenAIProvider
		if _, err := p.CallWithRetry(ctx, responses.ResponseNewParams{}); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("want ErrProviderUnavailable, got %v", err)
		}
	})

	t.Run("openai image", func(t *testing.T) {
		var p *OpenAIProvider
		if _, err := p.GenerateImagePNGBase64(ctx, "x"); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("want ErrProviderUnavailable, got %v", err)
		}
	})

	t.Run("openai file attachment", func(t *testing.T) {
		var p *OpenAIProvider
		if err := p.DeleteFileAttachment(ctx, "file-1"); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("want ErrProviderUnavailable, got %v", err)
		}
	})

	t.Run("openai token count", func(t *testing.T) {
		var p *OpenAIProvider
		if _, err := p.CountTokens("x"); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("want ErrProviderUnavailable, got %v", err)
		}
	})

	t.Run("claude call", func(t *testing.T) {
		var p *ClaudeProvider
		if _, err := p.Call(ctx, anthropic.MessageNewParams{}); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("want ErrProviderUnavailable, got %v", err)
		}
	})
}

// A constructed provider whose client is missing is the same hazard as a nil
// receiver for any method that dereferences it.
func TestProviderWithNoClientReturnsError(t *testing.T) {
	p := &OpenAIProvider{}
	if _, err := p.CallWithRetry(context.Background(), responses.ResponseNewParams{}); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("want ErrProviderUnavailable, got %v", err)
	}
}

// But a nil client is legitimate where the method never reaches it. Image
// generation results are persisted straight from the response, so a provider
// built without a client must still be able to save them — guarding the client
// there would reject a supported call, which is how this was caught.
func TestSaveMessageAttachmentsAcceptsNilClient(t *testing.T) {
	p := &OpenAIProvider{}
	err := p.SaveMessageAttachments(context.Background(), uuid.New(), uuid.New(), &responses.Response{})
	if errors.Is(err, ErrProviderUnavailable) {
		t.Fatal("a nil client must not block a path that never uses it")
	}
}
