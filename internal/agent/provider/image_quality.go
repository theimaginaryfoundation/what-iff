package provider

// ImageQuality is the requested quality tier for a generated image. It maps to
// the provider's image-model quality parameter (see buildImageGenerateParams)
type ImageQuality string

const (
	ImageQualityLow    ImageQuality = "low"
	ImageQualityMedium ImageQuality = "medium"
	ImageQualityHigh   ImageQuality = "high"
)

// ImageEngine identifies the image-generation model used for every request. It
// is reported to the usage seam alongside the quality tier
const ImageEngine = "gpt-image-1.5"
