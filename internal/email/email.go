// Package email sends transactional notifications via Amazon SES v2. The account-export worker uses
// it to tell a user their export bundle is ready, OpenAI-style, with a download link.
package email

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"go.uber.org/zap"
)

// ExportReadyData is the template data for the "your export is ready" email.
type ExportReadyData struct {
	Username   string
	BundleURL  string
	ExpiresAt  time.Time
	FilesCount int
}

// Sender delivers export notifications. Implementations: SES (prod) and Noop (local/dev).
type Sender interface {
	SendExportReady(ctx context.Context, to string, data ExportReadyData) error
}

// ── SES implementation ──────────────────────────────────────────────────────────

type sesSender struct {
	client      *sesv2.Client
	fromAddress string
	logger      *zap.Logger
}

// NewSESSender builds an SES-backed Sender. region defaults to the SDK chain when empty. fromAddress
// must be a verified SES identity (we have production access via the Cognito setup).
func NewSESSender(ctx context.Context, region, fromAddress string, logger *zap.Logger) (Sender, error) {
	if strings.TrimSpace(fromAddress) == "" {
		return nil, fmt.Errorf("email: EXPORT_FROM_EMAIL (SES from address) is required")
	}
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("email: loading AWS config: %w", err)
	}
	return &sesSender{client: sesv2.NewFromConfig(cfg), fromAddress: fromAddress, logger: logger}, nil
}

func (s *sesSender) SendExportReady(ctx context.Context, to string, data ExportReadyData) error {
	subject := "Your whatiff account export is ready"
	text, html := renderExportReady(data)
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.fromAddress),
		Destination:      &sestypes.Destination{ToAddresses: []string{to}},
		Content: &sestypes.EmailContent{
			Simple: &sestypes.Message{
				Subject: &sestypes.Content{Data: aws.String(subject)},
				Body: &sestypes.Body{
					Text: &sestypes.Content{Data: aws.String(text)},
					Html: &sestypes.Content{Data: aws.String(html)},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("email: SES SendEmail to %s: %w", to, err)
	}
	s.logger.Info("sent export-ready email", zap.String("to", to))
	return nil
}

// ── Noop implementation (local/dev) ──────────────────────────────────────────────

// NoopSender logs instead of sending — used when SES is not configured (local dev).
type NoopSender struct{ Logger *zap.Logger }

func (n NoopSender) SendExportReady(_ context.Context, to string, data ExportReadyData) error {
	if n.Logger != nil {
		n.Logger.Info("export-ready email (noop; SES not configured)",
			zap.String("to", to), zap.String("bundle_url", data.BundleURL))
	}
	return nil
}

// renderExportReady builds the plaintext and HTML bodies. Kept deliberately simple and
// self-contained; the link is the payload.
func renderExportReady(data ExportReadyData) (text, html string) {
	greeting := "Hi"
	if strings.TrimSpace(data.Username) != "" {
		greeting = "Hi " + data.Username
	}
	expires := "soon"
	if !data.ExpiresAt.IsZero() {
		expires = data.ExpiresAt.UTC().Format("2006-01-02 15:04 MST")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s,\n\n", greeting)
	b.WriteString("Your account export is ready. It's a ZIP containing your conversations, ")
	b.WriteString("personality scratchpads, and memories.\n\n")
	fmt.Fprintf(&b, "Download: %s\n", data.BundleURL)
	fmt.Fprintf(&b, "This link expires at %s.\n\n", expires)
	b.WriteString("For your security, this download link is only sent here by email — it is not shown ")
	b.WriteString("in the app — so access to your account alone cannot download your full data.\n\n")
	if data.FilesCount > 0 {
		fmt.Fprintf(&b, "An inventory of your %d uploaded files is included as files/manifest.json.\n\n", data.FilesCount)
	}
	b.WriteString("— whatiff\n")
	text = b.String()

	html = fmt.Sprintf(`<p>%s,</p>
<p>Your account export is ready. It's a ZIP containing your conversations, personality
scratchpads, and memories.</p>
<p><a href="%s">Download your export</a><br>
This link expires at %s.</p>
<p>For your security, this download link is only sent here by email — it is not shown in the app —
so access to your account alone cannot download your full data.</p>
<p>— whatiff</p>`, htmlEscape(greeting), htmlAttr(data.BundleURL), htmlEscape(expires))
	return text, html
}

// htmlEscape / htmlAttr are minimal escapers for the small set of interpolated values.
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func htmlAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "\"", "&quot;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
