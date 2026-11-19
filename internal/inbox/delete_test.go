package inbox

import "testing"

func TestDeleteRemovesIDsAndAll(t *testing.T) {
	in := Open(t.TempDir()+"/inbox.json", nil)
	a, _, _ := in.Publish("price.alert.fired", "stock", "600036.SH", alert(1), "a1", Options{})
	b, _, _ := in.Publish("price.alert.fired", "stock", "000001.SZ", alert(2), "a2", Options{})
	if a == nil || b == nil {
		t.Fatal("publish failed")
	}

	if n := in.Delete([]string{a.ID}, false); n != 1 {
		t.Fatalf("delete one = %d", n)
	}
	if got := in.List("", false, 0); len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("one item should remain: %+v", got)
	}
	if n := in.Delete(nil, true); n != 1 {
		t.Fatalf("delete all = %d", n)
	}
	if len(in.List("", false, 0)) != 0 || in.Unread() != 0 {
		t.Fatal("everything should be gone")
	}
}
