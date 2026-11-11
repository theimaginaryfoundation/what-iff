package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// Reference images for generate_image (ADR 0x045): the model names images it can see in the
// conversation, and the tool hands their bytes, with what each is for, to a vendor that edits.

// maxReferenceImageBytes caps one input image. Uploads are normalized to 2048px, so this only
// stops an oversized original from being sent to a vendor.
const maxReferenceImageBytes = 20 << 20

// generateImageReference is one entry of generate_image's reference_images: an image's name (or
// id) and what it is for. A bare string is accepted as a name with the default role.
type generateImageReference struct {
	Image string `json:"image"`
	Role  string `json:"role,omitempty"`
}

func (r *generateImageReference) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		r.Image = name
		return nil
	}
	type plain generateImageReference
	return json.Unmarshal(data, (*plain)(r))
}

// referenceRoles are the roles the tool accepts, by their tool-facing names.
var referenceRoles = map[string]provider.ImageRole{
	"":          provider.ImageRoleReference,
	"base":      provider.ImageRoleBase,
	"edit":      provider.ImageRoleBase,
	"markup":    provider.ImageRoleMarkup,
	"sketch":    provider.ImageRoleSketch,
	"reference": provider.ImageRoleReference,
	"style":     provider.ImageRoleReference,
}

// resolvedImage is a named image's bytes and what the model sees it as.
type resolvedImage struct {
	name string
	data []byte
	mime string
}

// imageInputs resolves the tool's reference_images and mask_image into a vendor request's images
// and mask. The base image goes first; a mask without one makes the first reference the base.
func (a *Agent) imageInputs(ctx context.Context, chat *models.Chat, refs []generateImageReference, maskName string) ([]provider.ImageInput, []byte, error) {
	if len(refs) > provider.MaxEditImages {
		return nil, nil, fmt.Errorf("at most %d reference_images are supported", provider.MaxEditImages)
	}
	resolver := a.newImageResolver(ctx, chat)
	inputs := make([]provider.ImageInput, 0, len(refs))
	bases := 0
	for _, ref := range refs {
		role, ok := referenceRoles[strings.ToLower(strings.TrimSpace(ref.Role))]
		if !ok {
			return nil, nil, fmt.Errorf("invalid role %q for %s (expected base, markup, sketch or reference)", ref.Role, ref.Image)
		}
		img, err := resolver.resolve(ref.Image)
		if err != nil {
			return nil, nil, err
		}
		input := provider.ImageInput{Data: img.data, MIMEType: img.mime, Role: role}
		if role == provider.ImageRoleBase {
			if bases++; bases > 1 {
				return nil, nil, fmt.Errorf("only one reference image can be the base")
			}
			inputs = append([]provider.ImageInput{input}, inputs...)
			continue
		}
		inputs = append(inputs, input)
	}
	var mask []byte
	if strings.TrimSpace(maskName) != "" {
		if len(inputs) == 0 {
			return nil, nil, fmt.Errorf("mask_image needs the image it masks in reference_images")
		}
		img, err := resolver.resolve(maskName)
		if err != nil {
			return nil, nil, err
		}
		mask = img.data
		if inputs[0].Role != provider.ImageRoleBase {
			inputs[0].Role = provider.ImageRoleBase
		}
	}
	return inputs, mask, nil
}

// imageResolver finds images by the names the model saw: images generated earlier in this turn,
// then the conversation's context files (ADR 0x043 §4), then the user's files by id. It lists the
// context files once, on first use.
type imageResolver struct {
	a      *Agent
	ctx    context.Context
	chat   *models.Chat
	files  []ContextFile
	listed bool
}

func (a *Agent) newImageResolver(ctx context.Context, chat *models.Chat) *imageResolver {
	return &imageResolver{a: a, ctx: ctx, chat: chat}
}

func (r *imageResolver) resolve(name string) (resolvedImage, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return resolvedImage{}, fmt.Errorf("an image name is empty")
	}
	if chatCtx := turnChatContextFrom(r.ctx); chatCtx != nil {
		if att := chatCtx.turnImage(name); att != nil {
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(att.FileContent))
			if err == nil && len(raw) > 0 {
				return checkedImage(att.Name, raw, att.FileType)
			}
		}
	}
	if att := r.contextImage(name); att != nil {
		return r.load(att)
	}
	if id, err := uuid.Parse(name); err == nil && r.a.ds != nil {
		att, err := r.a.ds.GetFileAttachment(r.ctx, r.chat.UserID, id)
		if err == nil && att != nil && strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			return r.load(att)
		}
	}
	return resolvedImage{}, fmt.Errorf("no image named %q in this conversation%s", name, r.knownNames())
}

// contextImage is the context file that takes name as its name or workspace path, ignoring case.
func (r *imageResolver) contextImage(name string) *models.FileAttachment {
	for _, f := range r.contextFiles() {
		if f.ShadowedBy != nil || f.Attachment == nil || !strings.HasPrefix(f.Attachment.FileType, models.ImageMIMEPrefix) {
			continue
		}
		if strings.EqualFold(f.Attachment.Name, name) || strings.EqualFold(f.WorkspacePath, name) || f.Attachment.ID.String() == name {
			return f.Attachment
		}
	}
	return nil
}

func (r *imageResolver) contextFiles() []ContextFile {
	if r.listed {
		return r.files
	}
	r.listed = true
	files, err := r.a.ContextFilesForChat(r.ctx, r.chat)
	if err != nil {
		r.a.logger.Warn("generate_image: could not list the conversation's files", zap.Error(err))
	}
	r.files = files
	return files
}

func (r *imageResolver) load(att *models.FileAttachment) (resolvedImage, error) {
	raw, mime, _ := resolveImageBytes(r.ctx, r.a.logger, r.a.fileStore, r.chat.UserID, r.chat.ID, att, false)
	if len(raw) == 0 {
		return resolvedImage{}, fmt.Errorf("could not read %s", att.Name)
	}
	return checkedImage(att.Name, raw, mime)
}

// checkedImage refuses what is not an image or too large to send, and types it by its bytes.
func checkedImage(name string, raw []byte, declared string) (resolvedImage, error) {
	if len(raw) > maxReferenceImageBytes {
		return resolvedImage{}, fmt.Errorf("%s is too large to use as a reference (over %d MB)", name, maxReferenceImageBytes>>20)
	}
	mime := http.DetectContentType(raw)
	if !strings.HasPrefix(mime, "image/") {
		return resolvedImage{}, fmt.Errorf("%s is not an image", name)
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
		return resolvedImage{}, fmt.Errorf("%s is %s; reference images must be PNG, JPEG or WebP", name, declared)
	}
	return resolvedImage{name: name, data: raw, mime: mime}, nil
}

// knownNames lists a few image names the model could have meant, for the error.
func (r *imageResolver) knownNames() string {
	var names []string
	if chatCtx := turnChatContextFrom(r.ctx); chatCtx != nil {
		names = append(names, chatCtx.turnImageNames()...)
	}
	for _, f := range r.contextFiles() {
		if f.ShadowedBy == nil && f.Attachment != nil && strings.HasPrefix(f.Attachment.FileType, models.ImageMIMEPrefix) {
			names = append(names, f.Attachment.Name)
		}
	}
	if len(names) == 0 {
		return " (there are no images in it)"
	}
	sort.Strings(names)
	if len(names) > 10 {
		names = append(names[:10], "…")
	}
	return "; images here: " + strings.Join(names, ", ")
}

// rememberTurnImages keeps the images a tool made this turn for later reference_images.
func (c *chatContext) rememberTurnImages(atts []*models.FileAttachment) {
	if c == nil {
		return
	}
	c.turnImagesMu.Lock()
	defer c.turnImagesMu.Unlock()
	for _, att := range atts {
		if att != nil && strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) && att.FileContent != "" {
			c.turnImages = append(c.turnImages, att)
		}
	}
}

// turnImage is the image made this turn under name (ignoring case), or nil.
func (c *chatContext) turnImage(name string) *models.FileAttachment {
	c.turnImagesMu.Lock()
	defer c.turnImagesMu.Unlock()
	for _, att := range c.turnImages {
		if strings.EqualFold(att.Name, name) {
			return att
		}
	}
	return nil
}

func (c *chatContext) turnImageNames() []string {
	c.turnImagesMu.Lock()
	defer c.turnImagesMu.Unlock()
	names := make([]string, 0, len(c.turnImages))
	for _, att := range c.turnImages {
		names = append(names, att.Name)
	}
	return names
}

// imageEditorForUser returns the user's image vendor as an editor. A request with input images
// never moves to another vendor (ADR 0x045 §5): that could cost more than the user chose, or send
// their images somewhere they did not expect. A vendor that cannot edit is refused with
// noImageEditorError, which the agent passes on.
func (a *Agent) imageEditorForUser(ctx context.Context, userID uuid.UUID) (provider.ImageEditor, error) {
	gen, err := a.imageGeneratorForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if ed, ok := gen.(provider.ImageEditor); ok {
		return ed, nil
	}
	return nil, noImageEditorError(gen.Provider())
}

// noImageEditorError tells the model the user's vendor cannot take input images, and what the user
// can do about it.
func noImageEditorError(p models.ModelProvider) error {
	return fmt.Errorf("the image generation provider selected in Settings (%s) cannot edit images or use reference images or masks; "+
		"tell the user, and that choosing OpenAI or Google under Settings → Image generation enables it. "+
		"Do not retry with reference_images; generating from a text prompt alone still works", imageProviderName(p))
}

// imageProviderName is a vendor as the settings UI names it.
func imageProviderName(p models.ModelProvider) string {
	switch p {
	case models.ModelProviderOpenAI:
		return "OpenAI"
	case models.ModelProviderGoogle:
		return "Google"
	case models.ModelProviderZAI:
		return "Z.ai"
	case models.ModelProviderQwen:
		return "Qwen"
	default:
		return string(p)
	}
}
