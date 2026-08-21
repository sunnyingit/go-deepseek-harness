package fs

import "testing"

func TestUnconditionalNeverFails(t *testing.T) {
	if err := Unconditional().Check(false, ""); err != nil {
		t.Fatal(err)
	}
	if err := Unconditional().Check(true, "v1"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateIfAbsentFailsWhenTargetExists(t *testing.T) {
	if err := CreateIfAbsent().Check(false, ""); err != nil {
		t.Fatalf("want no error when the target is genuinely absent: %v", err)
	}
	err := CreateIfAbsent().Check(true, "v1")
	var fsErr *Error
	if err == nil {
		t.Fatal("want an error when the target already exists")
	}
	if e, ok := err.(*Error); !ok || e.Code != CodeAlreadyExists {
		t.Fatalf("err = %v, want CodeAlreadyExists", err)
	}
	_ = fsErr
}

func TestReplaceIfVersionMatchesCurrentVersion(t *testing.T) {
	guard := ReplaceIfVersion("v1")
	if err := guard.Check(true, "v1"); err != nil {
		t.Fatalf("matching version should pass: %v", err)
	}
}

func TestReplaceIfVersionFailsOnMismatch(t *testing.T) {
	guard := ReplaceIfVersion("v1")
	err := guard.Check(true, "v2")
	e, ok := err.(*Error)
	if !ok || e.Code != CodeStaleVersion {
		t.Fatalf("err = %v, want CodeStaleVersion", err)
	}
}

func TestReplaceIfVersionFailsWhenTargetGone(t *testing.T) {
	guard := ReplaceIfVersion("v1")
	err := guard.Check(false, "")
	e, ok := err.(*Error)
	if !ok || e.Code != CodeStaleVersion {
		t.Fatalf("err = %v, want CodeStaleVersion for a target that no longer exists", err)
	}
}

func TestErrorFormatting(t *testing.T) {
	err := &Error{Code: CodeNotFound, Message: "no such file"}
	if err.Error() != "FS_NOT_FOUND: no such file" {
		t.Fatalf("Error() = %q", err.Error())
	}
}
