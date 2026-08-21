# attachment

A Go port of [`packages/attachment/attachment`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/attachment/attachment/src)
(`dsh-attachment`) from DeepSeek Harness: the abstract contract for
durable, content-addressed image storage. This package defines the
contract and its one universal validation rule; it stores nothing itself.

> Consumers never persist a browser path, an object URL, a provider URL, or
> raw base64 in a session event — only the opaque `ImageAttachmentRef` a
> `Store` hands back after admitting and saving an image.

This package is stdlib-only — no dependency on any other package in this
module.

## Ported

- **The vocabulary** (`types.go`): `ImageMediaType` (PNG/JPEG/WebP/GIF —
  v1's exact scope, matching the TS original), `AttachmentID`,
  `ImageAttachmentRef` (durable metadata: id, verified media type, byte
  count, intrinsic width/height, optional display name),
  `EncodedImageAttachment` (the base64 wire form), `SaveImageAttachment`
  /`StoredImageAttachment` (the decoded-bytes form a `Store` actually
  works with), `ImageAttachmentLimits` (the deployment-resolved constraints
  a `Store` enforces — declared here as shared vocabulary, not read or
  enforced by this package itself).
- **`Store`**: the interface an implementation provides —
  `SaveImages` (atomic: a validation failure on any batch member returns no
  references at all, not a partial batch — "no partial references") and
  `ReadImage` (verifies the content-addressed object against its logged
  metadata before returning it).
- **`AdmitEncodedImages`**: the one orchestration function this package
  exports — decodes each image's base64, then delegates the batch to
  `Store.SaveImages`. Stops at the first invalid member without ever
  calling `SaveImages`, matching `TestAdmitEncodedImagesStopsAtFirstInvalidMemberWithoutSavingAny`.
- **Canonical base64 validation**: the *one* real check this package
  performs itself (`decodeCanonicalBase64`) — decode, then re-encode and
  compare against the original string; a non-canonical encoding (valid
  bytes, wrong padding bits — e.g. `"AB=="` decodes to the same byte as
  `"AA=="` but isn't its canonical form) is rejected with
  `CodeInvalidImageBase64`. This matters specifically for content
  -addressing: two different strings that decode to the same bytes must
  not be treated as different inputs.
- **Error codes** (`error.go`): the full `AttachmentErrorCode` union —
  nine image-admission codes (`TOO_MANY_IMAGES`, `IMAGES_TOO_LARGE`,
  `UNSUPPORTED_IMAGE_TYPE`, `INVALID_IMAGE_BASE64`, `INVALID_IMAGE`,
  `IMAGE_TYPE_MISMATCH`, `IMAGE_TOO_LARGE`, `IMAGE_TOO_MANY_PIXELS`,
  `IMAGE_DIMENSION_TOO_LARGE`) plus five storage/retrieval codes
  (`INVALID_ATTACHMENT_REF`, `ATTACHMENT_CORRUPT`,
  `ATTACHMENT_WRITE_FAILED`, `ATTACHMENT_NOT_FOUND`,
  `ATTACHMENT_READ_FAILED`). `IsImageAdmissionError` reports membership in
  exactly the first (caller-correctable) set, mirroring the TS original's
  own type-guard split.

## Deliberate simplifications vs. the TS original

- **No implementation** (`attachment-local` isn't ported): this package
  defines what a `Store` must do, not one that actually does it. Every
  admission rule beyond canonical base64 — true-format verification
  (magic-byte sniffing against the declared `MediaType`), dimension
  /pixel-count extraction, and every `ImageAttachmentLimits` check — is
  documented in the TS original as the *store's* job, not
  `admitEncodedImages`'s, so a Go `Store` implementation is exactly as
  out of scope here as it is in the upstream package split.
- **No "branded type" for `AttachmentID`**: TS uses an intersection-type
  trick (`string & { readonly __brand: 'AttachmentId' }`) to stop a caller
  from accidentally passing a raw string where an ID is expected, since TS
  string types are structurally interchangeable otherwise. Go's nominal
  typing already gives a `type AttachmentID string` this exact distinction
  for free — a plain string doesn't satisfy a function expecting an
  `AttachmentID` without an explicit conversion — so there's no `brand.go`
  needed to reproduce a trick Go doesn't need in the first place.
- **`context.Context` instead of a cancellation token**: `ReadImage`
  /`SaveImages` take `ctx context.Context`, matching every other package in
  this module, instead of the TS original's separate cancellation
  -token parameter.

## Run

```sh
go build ./...
go test ./... -race -v
```
