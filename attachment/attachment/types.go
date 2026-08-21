// Package attachment is a Go port of packages/attachment/attachment
// (dsh-attachment) from DeepSeek Harness: the abstract contract for
// durable, content-addressed image storage. Consumers (e.g. a read_image
// tool) never persist a browser path, an object URL, a provider URL, or
// raw base64 in a session event — only the opaque ImageAttachmentRef a
// Store hands back after admitting and saving an image.
//
// This package defines the contract and its one universal validation rule
// (canonical base64); an implementation (this port has none yet — see
// README.md) does the real work: true-format verification, dimension
// extraction, and every size/count/media-type limit.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package attachment

// ImageMediaType is a supported image format. v1 is exactly these four —
// matching the TS original's own "PNG/JPEG/WebP/GIF only" scope.
type ImageMediaType string

const (
	MediaPNG  ImageMediaType = "image/png"
	MediaJPEG ImageMediaType = "image/jpeg"
	MediaWebP ImageMediaType = "image/webp"
	MediaGIF  ImageMediaType = "image/gif"
)

// AttachmentID is an opaque storage identifier — never a filesystem path or
// bearer URL. A plain Go string type already gives this the nominal
// distinction TS needs an artificial "branded type" trick for for; see
// README.md.
type AttachmentID string

// ImageAttachmentRef is durable, content-addressed metadata for an
// immutable image object — what a caller actually keeps (in a session
// event, a tool result) instead of the image bytes themselves.
type ImageAttachmentRef struct {
	AttachmentID AttachmentID
	// MediaType is verified from the stored bytes, not merely the caller's
	// declaration — see SaveImageAttachment.
	MediaType ImageMediaType
	Bytes     int    // exact encoded byte length
	Width     int    // intrinsic encoded width, in pixels
	Height    int    // intrinsic encoded height, in pixels
	Name      string // "" if absent; a display name, never interpreted as a path
}

// EncodedImageAttachment is a base64-encoded image accompanying a wire
// request — AdmitEncodedImages' input shape.
type EncodedImageAttachment struct {
	MediaType ImageMediaType // declared; verified against the decoded bytes by the Store
	Data      string         // canonical base64 encoding of the image bytes
	Name      string
}

// ImageAttachmentLimits are deployment-resolved constraints a Store
// enforces during SaveImages. Declared here as part of the shared
// vocabulary; this package itself doesn't read or enforce them — see
// README.md.
type ImageAttachmentLimits struct {
	MaxImageBytes        int
	MaxImagesPerMessage  int
	MaxMessageImageBytes int
	MaxImagePixels       int
	MaxImageDimension    int // maximum intrinsic width and height, in pixels
	MediaTypes           []ImageMediaType
}

// SaveImageAttachment is a Store.SaveImages request: already-decoded bytes
// plus the caller's declared media type (checked against the fully decoded
// bytes by the Store, not trusted).
type SaveImageAttachment struct {
	Data      []byte
	MediaType ImageMediaType
	Name      string // browser/provider display name, never interpreted as a path
}

// StoredImageAttachment is one committed result: the durable reference plus
// the exact bytes that were verified and stored.
type StoredImageAttachment struct {
	Ref  ImageAttachmentRef
	Data []byte
}
