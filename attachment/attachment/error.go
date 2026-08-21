package attachment

import "errors"

// ErrorCode is a stable, model-facing failure classification — mirrors the
// TS original's AttachmentErrorCode union.
type ErrorCode string

// Image admission codes: caller-correctable — the request itself was bad
// (too many images, wrong type, corrupt bytes, over a limit), not a
// storage-layer problem. IsImageAdmissionError checks membership in
// exactly this set.
const (
	CodeTooManyImages          ErrorCode = "TOO_MANY_IMAGES"
	CodeImagesTooLarge         ErrorCode = "IMAGES_TOO_LARGE"
	CodeUnsupportedImageType   ErrorCode = "UNSUPPORTED_IMAGE_TYPE"
	CodeInvalidImageBase64     ErrorCode = "INVALID_IMAGE_BASE64"
	CodeInvalidImage           ErrorCode = "INVALID_IMAGE"
	CodeImageTypeMismatch      ErrorCode = "IMAGE_TYPE_MISMATCH"
	CodeImageTooLarge          ErrorCode = "IMAGE_TOO_LARGE"
	CodeImageTooManyPixels     ErrorCode = "IMAGE_TOO_MANY_PIXELS"
	CodeImageDimensionTooLarge ErrorCode = "IMAGE_DIMENSION_TOO_LARGE"
)

// Storage/retrieval codes: the request was admissible, but the Store
// itself failed to honor it.
const (
	CodeInvalidAttachmentRef  ErrorCode = "INVALID_ATTACHMENT_REF"
	CodeAttachmentCorrupt     ErrorCode = "ATTACHMENT_CORRUPT"
	CodeAttachmentWriteFailed ErrorCode = "ATTACHMENT_WRITE_FAILED"
	CodeAttachmentNotFound    ErrorCode = "ATTACHMENT_NOT_FOUND"
	CodeAttachmentReadFailed  ErrorCode = "ATTACHMENT_READ_FAILED"
)

// imageAdmissionCodes is the closed set IsImageAdmissionError checks
// membership against.
var imageAdmissionCodes = map[ErrorCode]bool{
	CodeTooManyImages: true, CodeImagesTooLarge: true, CodeUnsupportedImageType: true,
	CodeInvalidImageBase64: true, CodeInvalidImage: true, CodeImageTypeMismatch: true,
	CodeImageTooLarge: true, CodeImageTooManyPixels: true, CodeImageDimensionTooLarge: true,
}

// Error is a structured attachment failure: Code is stable and
// switch-on-able, Message is the human/model-facing detail, Err is an
// optional wrapped cause (errors.Unwrap-able).
type Error struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }
func (e *Error) Unwrap() error { return e.Err }

// IsImageAdmissionError reports whether err is an *Error whose Code is one
// of the caller-correctable image-admission codes, as opposed to a
// storage/retrieval failure the caller can't fix by changing its request.
func IsImageAdmissionError(err error) bool {
	var attErr *Error
	if !errors.As(err, &attErr) {
		return false
	}
	return imageAdmissionCodes[attErr.Code]
}
