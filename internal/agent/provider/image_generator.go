package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// GeneratedImage is one image produced by an ImageGenerator.
type GeneratedImage struct {
	// Base64 is the raw base64 payload (no data: prefix).
	Base64 string
	// MIMEType is the image's actual format. Vendors do not all return PNG
	// (Gemini answers with JPEG), so callers must not assume one.
	MIMEType string
}

// ImageGenerator is one vendor's text-to-image endpoint. The generate_image tool
// and the image ritual use whichever one the user picked (UserPreference
// image_generation_provider); see Agent.imageGeneratorForUser.
//
// Quality and aspect ratio are the tool's vendor-neutral knobs. Each adapter maps
// them to the closest thing its API has and ignores what it cannot express.
type ImageGenerator interface {
	// Provider is the vendor this generator calls.
	Provider() models.ModelProvider
	// Model is the vendor model id, reported to metering as the engine.
	Model() string
	GenerateImage(ctx context.Context, prompt string, quality ImageQuality, aspectRatio ImageAspectRatio) (GeneratedImage, error)
}

// ImageGenerationProviders lists the vendors an ImageGenerator exists for, in the
// order the settings UI offers them. OpenAI is first because it is the default.
var ImageGenerationProviders = []models.ModelProvider{
	models.ModelProviderOpenAI,
	models.ModelProviderGoogle,
	models.ModelProviderZAI,
	models.ModelProviderQwen,
}

// IsImageGenerationProvider reports whether p has an ImageGenerator.
func IsImageGenerationProvider(p models.ModelProvider) bool {
	for _, known := range ImageGenerationProviders {
		if known == p {
			return true
		}
	}
	return false
}

// openAIImageGenerator adapts OpenAIProvider's Images API calls to ImageGenerator.
type openAIImageGenerator struct{ p *OpenAIProvider }

// ImageGenerator returns the OpenAI Images API (ImageEngine) as an ImageGenerator.
func (a *OpenAIProvider) ImageGenerator() ImageGenerator { return openAIImageGenerator{p: a} }

func (g openAIImageGenerator) Provider() models.ModelProvider { return models.ModelProviderOpenAI }
func (g openAIImageGenerator) Model() string                  { return ImageEngine }

func (g openAIImageGenerator) GenerateImage(ctx context.Context, prompt string, quality ImageQuality, aspectRatio ImageAspectRatio) (GeneratedImage, error) {
	b64, err := g.p.GenerateImagePNGBase64WithOptions(ctx, prompt, quality, aspectRatio)
	if err != nil {
		return GeneratedImage{}, err
	}
	return GeneratedImage{Base64: b64, MIMEType: "image/png"}, nil
}

// PrepareEdit checks req and turns its mask into OpenAI's form (transparent where the base may
// change, at the base's size).
func (g openAIImageGenerator) PrepareEdit(req ImageEditRequest) (ImageEditRequest, error) {
	return req.prepared("openai", func(r ImageEditRequest) ([]byte, error) {
		w, h, err := imageSize(r.Images[0].Data)
		if err != nil {
			return nil, fmt.Errorf("base image: %w", err)
		}
		return alphaMaskPNG(r.Mask, w, h)
	})
}

// EditImage is the Images API edit endpoint with every input image, and the mask as OpenAI wants it.
// gpt-image treats the mask as strong guidance rather than a hard stencil, so the instructions
// repeat it.
func (g openAIImageGenerator) EditImage(ctx context.Context, req ImageEditRequest) (GeneratedImage, error) {
	req, err := g.PrepareEdit(req)
	if err != nil {
		return GeneratedImage{}, err
	}
	mask := req.vendorMask
	prompt := editInstructions(req.Images, 0, mask != nil, req.Prompt)
	b64, err := g.p.editImage(ctx, buildImageEditRequestParams(prompt, req.Quality, req.editAspectRatio(), req.Images, mask))
	if err != nil {
		return GeneratedImage{}, err
	}
	return GeneratedImage{Base64: b64, MIMEType: "image/png"}, nil
}

// maxDownloadedImageBytes caps an image fetched from a vendor's result URL.
const maxDownloadedImageBytes = 32 << 20

// imageHTTPClient is the plumbing the HTTP-only adapters share: a JSON POST and,
// for vendors that answer with a URL, a download of the result.
type imageHTTPClient struct {
	client *http.Client
	// retryDelays overrides defaultImageRetryDelays and defaultDownloadRetryDelays (tests).
	retryDelays []time.Duration
}

func (c imageHTTPClient) httpClient() *http.Client {
	if c.client != nil {
		return c.client
	}
	return http.DefaultClient
}

// defaultImageRetryDelays are the waits before each retry of a rate-limited or
// overloaded image request. Image endpoints allow little concurrency (Qwen
// answers 429 to a handful of parallel requests), and generate_image sends up
// to four at once, so a short backoff turns most 429s into images.
var defaultImageRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second}

// maxImageRetryAfter caps a server-requested Retry-After.
const maxImageRetryAfter = 15 * time.Second

// imageResponse is one raw answer from an image endpoint.
type imageResponse struct {
	body   []byte
	status int
	// retryAfter is the server's Retry-After in whole seconds; HTTP-date values
	// are ignored and the default backoff applies.
	retryAfter time.Duration
}

func (c imageHTTPClient) delays() []time.Duration {
	if c.retryDelays != nil {
		return c.retryDelays
	}
	return defaultImageRetryDelays
}

// postJSON POSTs body to endpoint and decodes a 2xx response into out,
// retrying 429 and 503 after each of the client's retry delays. A non-2xx
// response is returned as *imageAPIError carrying the body, so adapters can
// recognise their vendor's content-policy refusals. Cancellation during a wait
// returns the context's error.
func (c imageHTTPClient) postJSON(ctx context.Context, endpoint string, headers map[string]string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode image request: %w", err)
	}
	delays := c.delays()
	for attempt := 0; ; attempt++ {
		resp, err := c.postOnce(ctx, endpoint, headers, payload)
		if err != nil {
			return err
		}
		if resp.status >= 200 && resp.status <= 299 {
			if err := json.Unmarshal(resp.body, out); err != nil {
				return fmt.Errorf("decode image response: %w", err)
			}
			return nil
		}
		apiErr := &imageAPIError{Status: resp.status, Body: string(resp.body)}
		retryable := resp.status == http.StatusTooManyRequests || resp.status == http.StatusServiceUnavailable
		if !retryable || attempt >= len(delays) {
			return apiErr
		}
		wait := delays[attempt]
		if resp.retryAfter > 0 {
			wait = min(resp.retryAfter, maxImageRetryAfter)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("image request cancelled while retrying after %w: %w", apiErr, ctx.Err())
		case <-timer.C:
		}
	}
}

func (c imageHTTPClient) postOnce(ctx context.Context, endpoint string, headers map[string]string, payload []byte) (imageResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return imageResponse{}, fmt.Errorf("build image request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return imageResponse{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadedImageBytes*2))
	if err != nil {
		return imageResponse{}, fmt.Errorf("read image response: %w", err)
	}
	out := imageResponse{body: raw, status: resp.StatusCode}
	if secs, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && secs > 0 {
		out.retryAfter = time.Duration(secs) * time.Second
	}
	return out, nil
}

// download fetches a result image and returns it base64-encoded with its
// sniffed MIME type. Result URLs are pre-signed object-store links on the
// vendor's CDN. The URL comes from the vendor's response, so only https is
// followed: a plain-http or other-scheme link is refused rather than fetched
// through the provider client.
func (c imageHTTPClient) download(ctx context.Context, rawURL string) (GeneratedImage, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return GeneratedImage{}, fmt.Errorf("download image: refusing non-https result URL")
	}
	delays := c.downloadDelays()
	for attempt := 0; ; attempt++ {
		img, status, err := c.downloadOnce(ctx, u.String())
		if err == nil {
			return img, nil
		}
		if !downloadRetryable(status) || attempt >= len(delays) {
			return GeneratedImage{}, err
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return GeneratedImage{}, fmt.Errorf("%w (gave up waiting: %w)", err, ctx.Err())
		case <-timer.C:
		}
	}
}

// defaultDownloadRetryDelays are the waits before re-fetching a result URL. z.ai answers with
// the URL a moment before its file CDN serves it, so an immediate fetch can 404; a few seconds
// of patience is enough.
var defaultDownloadRetryDelays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}

func (c imageHTTPClient) downloadDelays() []time.Duration {
	if c.retryDelays != nil {
		return c.retryDelays
	}
	return defaultDownloadRetryDelays
}

// downloadRetryable reports whether a result-URL status may clear up on its own: not there
// yet (404), rate limited, or a server error.
func downloadRetryable(status int) bool {
	return status == http.StatusNotFound || status == http.StatusTooManyRequests || status >= 500
}

// downloadOnce fetches the image once, returning the HTTP status (0 when no response arrived).
func (c imageHTTPClient) downloadOnce(ctx context.Context, rawURL string) (GeneratedImage, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return GeneratedImage{}, 0, fmt.Errorf("build image download: %w", err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return GeneratedImage{}, 0, fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return GeneratedImage{}, resp.StatusCode, fmt.Errorf("download image: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadedImageBytes+1))
	if err != nil {
		return GeneratedImage{}, resp.StatusCode, fmt.Errorf("download image: %w", err)
	}
	if len(data) > maxDownloadedImageBytes {
		return GeneratedImage{}, resp.StatusCode, fmt.Errorf("download image: larger than %d bytes", maxDownloadedImageBytes)
	}
	img, err := imageFromBytes(data)
	return img, resp.StatusCode, err
}

// imageFromBytes checks that data is an image and encodes it.
func imageFromBytes(data []byte) (GeneratedImage, error) {
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return GeneratedImage{}, fmt.Errorf("image response is %s, not an image", mime)
	}
	return GeneratedImage{Base64: base64.StdEncoding.EncodeToString(data), MIMEType: mime}, nil
}

// imageAPIError is a non-2xx answer from an image endpoint.
type imageAPIError struct {
	Status int
	Body   string
}

func (e *imageAPIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	return fmt.Sprintf("image API returned status %d: %s", e.Status, body)
}

// imageSafetyViolation builds the typed refusal the generate_image tool records
// as a safety event, for vendors whose refusal text the generic sniffer
// (ParseSafetyViolationError) does not know.
func imageSafetyViolation(p models.SafetyViolationProvider, code, message, raw string) error {
	if strings.TrimSpace(message) == "" {
		message = "The image request was blocked by the provider's content policy."
	}
	return &SafetyViolationError{Violation: &SafetyViolation{
		Provider:        p,
		ViolationType:   "content_policy",
		ProviderCode:    code,
		ProviderMessage: message,
		RawError:        raw,
	}}
}

// joinURL appends path to base with exactly one slash between them.
func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
