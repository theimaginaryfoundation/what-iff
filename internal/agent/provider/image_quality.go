package provider

// ImageQuality is the requested quality tier for a generated image. It maps to
// the provider's image-model quality parameter (see buildImageGenerateParams)
type ImageQuality string

const (
	ImageQualityLow    ImageQuality = "low"
	ImageQualityMedium ImageQuality = "medium"
	ImageQualityHigh   ImageQuality = "high"
)

// ImageAspectRatio is the requested aspect ratio for a generated image. It maps
// to the provider's image size parameter (see buildImageGenerateParams)
type ImageAspectRatio string

const (
	ImageAspectRatioSquare    ImageAspectRatio = "square"
	ImageAspectRatioLandscape ImageAspectRatio = "landscape"
	ImageAspectRatioPortrait  ImageAspectRatio = "portrait"
)

// ImageEngine identifies the image-generation model used for every request. It
// is reported to the usage seam alongside the quality tier
const ImageEngine = "gpt-image-2.5-flare"
