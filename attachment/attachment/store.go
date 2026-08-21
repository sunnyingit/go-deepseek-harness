package attachment

import (
	"context"
	"encoding/base64"
)

// Store is the abstract contract an attachment backend implements —
// content-addressed, durable image storage. This package defines the
// contract only; it stores nothing itself. An implementation enforces
// ImageAttachmentLimits, true-format verification (the declared MediaType
// checked against the actual decoded bytes, not trusted), and dimension
// /pixel-count extraction — none of which this package does, see README.md.
type Store interface {
	// SaveImages validates and durably commits batch in order, atomically:
	// a validation failure on any member returns no references at all, not
	// a partial batch ("no partial references" — the TS original's own
	// documented guarantee).
	SaveImages(ctx context.Context, batch []SaveImageAttachment) ([]ImageAttachmentRef, error)
	// ReadImage retrieves a previously stored image's bytes, verifying the
	// content-addressed object against its logged metadata before
	// returning it.
	ReadImage(ctx context.Context, ref ImageAttachmentRef) ([]byte, error)
}

// AdmitEncodedImages validates each of images' base64 encoding — the one
// admission rule this package itself enforces — then delegates the decoded
// batch to store.SaveImages, which enforces every other rule (count, size,
// media type, dimensions). Returns durable references in the same order as
// images, or the first validation/storage error encountered.
func AdmitEncodedImages(ctx context.Context, store Store, images []EncodedImageAttachment) ([]ImageAttachmentRef, error) {
	batch := make([]SaveImageAttachment, len(images))
	for i, img := range images {
		decoded, err := decodeCanonicalBase64(img.Data)
		if err != nil {
			return nil, err
		}
		batch[i] = SaveImageAttachment{Data: decoded, MediaType: img.MediaType, Name: img.Name}
	}
	return store.SaveImages(ctx, batch)
}

// decodeCanonicalBase64 decodes data and rejects any non-canonical encoding
// (alternate padding, a different alphabet, embedded whitespace) by
// re-encoding the decoded bytes and comparing against the original —
// exactly the TS original's own check in decodeBase64: "after decoding,
// re-encode and compare against the input; non-canonical forms are
// rejected."
func decodeCanonicalBase64(data string) ([]byte, error) {
	if data == "" {
		return nil, &Error{Code: CodeInvalidImageBase64, Message: "image data must not be empty"}
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, &Error{Code: CodeInvalidImageBase64, Message: "invalid base64 encoding", Err: err}
	}
	if base64.StdEncoding.EncodeToString(decoded) != data {
		return nil, &Error{Code: CodeInvalidImageBase64, Message: "base64 encoding is not canonical"}
	}
	return decoded, nil
}
