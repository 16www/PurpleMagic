package pvf

import (
	"os"
	"testing"
)

func TestDiskIndexValidityUsesContentHash(t *testing.T) {
	path, index, digest, _ := diskFixture(t, true)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A metadata-only change does not invalidate this generation.
	if err = os.Chtimes(path, info.ModTime().AddDate(0, 0, 1), info.ModTime().AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDisk(path, index, digest)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	// Same path, size and timestamps, but different bytes must be rejected.
	raw[len(raw)-1] ^= 1
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if d, err = OpenDisk(path, index, digest); err == nil {
		d.Close()
		t.Fatal("changed PVF accepted with stale index")
	}
}
