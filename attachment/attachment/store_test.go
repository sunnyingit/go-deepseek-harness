package attachment

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

// fakeStore is a minimal in-memory Store — this package defines the
// contract, not an implementation (see attachment-local in the real
// project), so its own tests need a test double to exercise
// AdmitEncodedImages against.
type fakeStore struct {
	saveErr    error
	savedBatch []SaveImageAttachment
}

func (f *fakeStore) SaveImages(_ context.Context, batch []SaveImageAttachment) ([]ImageAttachmentRef, error) {
	if f.saveErr != nil {
		return nil, f.saveErr
	}
	f.savedBatch = batch
	refs := make([]ImageAttachmentRef, len(batch))
	for i, img := range batch {
		refs[i] = ImageAttachmentRef{AttachmentID: AttachmentID("id-" + string(rune('a'+i))), MediaType: img.MediaType, Bytes: len(img.Data), Name: img.Name}
	}
	return refs, nil
}

func (f *fakeStore) ReadImage(_ context.Context, ref ImageAttachmentRef) ([]byte, error) {
	return nil, &Error{Code: CodeAttachmentNotFound, Message: string(ref.AttachmentID)}
}

func canonicalB64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func TestAdmitEncodedImagesDecodesAndDelegates(t *testing.T) {
	store := &fakeStore{}
	images := []EncodedImageAttachment{
		{MediaType: MediaPNG, Data: canonicalB64([]byte("fake-png-bytes")), Name: "a.png"},
		{MediaType: MediaJPEG, Data: canonicalB64([]byte("fake-jpeg-bytes")), Name: "b.jpg"},
	}

	refs, err := AdmitEncodedImages(context.Background(), store, images)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].Name != "a.png" || refs[1].Name != "b.jpg" {
		t.Fatalf("refs = %+v", refs)
	}
	if len(store.savedBatch) != 2 || string(store.savedBatch[0].Data) != "fake-png-bytes" {
		t.Fatalf("savedBatch = %+v, want the decoded bytes forwarded to the store", store.savedBatch)
	}
}

func TestAdmitEncodedImagesRejectsEmptyData(t *testing.T) {
	store := &fakeStore{}
	_, err := AdmitEncodedImages(context.Background(), store, []EncodedImageAttachment{{MediaType: MediaPNG, Data: ""}})
	var attErr *Error
	if !errors.As(err, &attErr) || attErr.Code != CodeInvalidImageBase64 {
		t.Fatalf("err = %v, want CodeInvalidImageBase64", err)
	}
}

func TestAdmitEncodedImagesRejectsInvalidBase64(t *testing.T) {
	store := &fakeStore{}
	_, err := AdmitEncodedImages(context.Background(), store, []EncodedImageAttachment{{MediaType: MediaPNG, Data: "not-valid-base64!!!"}})
	var attErr *Error
	if !errors.As(err, &attErr) || attErr.Code != CodeInvalidImageBase64 {
		t.Fatalf("err = %v, want CodeInvalidImageBase64", err)
	}
}

func TestAdmitEncodedImagesRejectsNonCanonicalBase64(t *testing.T) {
	// "AB==" decodes cleanly to a single zero byte (valid base64 alphabet,
	// valid padding), but its trailing 4 data bits are non-zero when the
	// spec requires them to be — the canonical re-encoding of that same
	// byte is "AA==", not "AB==". A decoder that accepts this without
	// checking canonical form would let two different strings alias the
	// same content-addressed bytes.
	nonCanonical := "AB=="
	if decoded, err := base64.StdEncoding.DecodeString(nonCanonical); err != nil || base64.StdEncoding.EncodeToString(decoded) == nonCanonical {
		t.Fatalf("test fixture assumption broke: decode err=%v, or the string is already canonical", err)
	}

	store := &fakeStore{}
	_, err := AdmitEncodedImages(context.Background(), store, []EncodedImageAttachment{{MediaType: MediaPNG, Data: nonCanonical}})
	var attErr *Error
	if !errors.As(err, &attErr) || attErr.Code != CodeInvalidImageBase64 {
		t.Fatalf("err = %v, want CodeInvalidImageBase64 for a non-canonical (but decodable) base64 string", err)
	}
}

func TestAdmitEncodedImagesStopsAtFirstInvalidMemberWithoutSavingAny(t *testing.T) {
	store := &fakeStore{}
	images := []EncodedImageAttachment{
		{MediaType: MediaPNG, Data: canonicalB64([]byte("ok"))},
		{MediaType: MediaPNG, Data: ""}, // invalid
	}
	_, err := AdmitEncodedImages(context.Background(), store, images)
	if err == nil {
		t.Fatal("want an error")
	}
	if store.savedBatch != nil {
		t.Fatal("SaveImages must never be called when any member fails validation first")
	}
}

func TestAdmitEncodedImagesPropagatesStoreError(t *testing.T) {
	store := &fakeStore{saveErr: &Error{Code: CodeImageTooLarge, Message: "too big"}}
	_, err := AdmitEncodedImages(context.Background(), store, []EncodedImageAttachment{{MediaType: MediaPNG, Data: canonicalB64([]byte("x"))}})
	var attErr *Error
	if !errors.As(err, &attErr) || attErr.Code != CodeImageTooLarge {
		t.Fatalf("err = %v, want CodeImageTooLarge propagated from the store", err)
	}
}

func TestIsImageAdmissionError(t *testing.T) {
	admission := &Error{Code: CodeImageTooLarge}
	storage := &Error{Code: CodeAttachmentNotFound}
	plain := errors.New("boom")

	if !IsImageAdmissionError(admission) {
		t.Fatal("want true for an admission code")
	}
	if IsImageAdmissionError(storage) {
		t.Fatal("want false for a storage/retrieval code")
	}
	if IsImageAdmissionError(plain) {
		t.Fatal("want false for a non-*Error")
	}
	if IsImageAdmissionError(nil) {
		t.Fatal("want false for nil")
	}
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := &Error{Code: CodeAttachmentReadFailed, Message: "read failed", Err: cause}
	if !errors.Is(err, cause) {
		t.Fatal("want errors.Is to see through to the wrapped cause")
	}
}

func TestErrorFormatting(t *testing.T) {
	err := &Error{Code: CodeInvalidImage, Message: "not a real image"}
	if err.Error() != "INVALID_IMAGE: not a real image" {
		t.Fatalf("Error() = %q", err.Error())
	}
}
