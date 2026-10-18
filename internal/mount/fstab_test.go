package mount

import (
	"bytes"
	"testing"
)

const uuid = "2A1B02AD-467D-403A-8CCD-B87E50AD3DA2"

func TestMergeIsIdempotent(t *testing.T) {
	e := Entry{UUID: uuid, Path: "/Users/developer/repos"}
	initial := []byte("# unrelated entry\nUUID=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa none apfs ro\n")
	out, changed, err := Merge(initial, e)
	if err != nil || !changed {
		t.Fatalf("expected addition: changed=%v, err=%v", changed, err)
	}
	again, changed, err := Merge(out, e)
	if err != nil || changed || !bytes.Equal(out, again) {
		t.Fatalf("repeated merge modified fstab: changed=%v, err=%v", changed, err)
	}
	if !bytes.HasPrefix(out, initial) {
		t.Fatal("unrelated existing entries must be preserved")
	}
}

func TestMergeConflicts(t *testing.T) {
	e := Entry{UUID: uuid, Path: "/Users/developer/repos"}
	for _, contents := range []string{
		"UUID=" + uuid + " /other apfs rw\n",
		"UUID=" + uuid + " /Users/developer/repos apfs rw,noauto\n",
		"UUID=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa /Users/developer/repos apfs rw\n",
		"invalid record\n",
	} {
		if _, _, err := Merge([]byte(contents), e); err == nil {
			t.Fatalf("expected conflict for %q", contents)
		}
	}
}

func TestSpaceEscaping(t *testing.T) {
	e := Entry{UUID: uuid, Path: "/Users/Test User/repos"}
	content, changed, err := Merge(nil, e)
	if err != nil || !changed || !bytes.Contains(content, []byte("/Users/Test\\040User/repos")) {
		t.Fatalf("escaped fstab record incorrect: %q, %v", content, err)
	}
}
