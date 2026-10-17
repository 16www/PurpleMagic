package pvf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func diskFixture(t *testing.T, protected bool) (string, string, string, *Archive) {
	t.Helper()
	a, err := OpenBytes(buildSyntheticNKPIArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	if protected {
		a.format = FormatProtectedNKPI
	}
	names := make([]byte, 8)
	for _, part := range []struct {
		key  string
		mask uint32
		raw  []byte
	}{{"sTrA", 0xAA74472E, a.strA}, {"sTrW", 0x9A82F037, a.strW}} {
		var encoded []byte
		if protected {
			key := "StRa"
			if part.key == "sTrW" {
				key = "StRw"
			}
			encoded, err = encodeProtectedStringBuffer(key, part.mask, part.raw)
		} else {
			var compressed []byte
			compressed, err = compressZlib(part.raw)
			decrypt2(part.key, compressed)
			encoded = make([]byte, 8)
			binary.LittleEndian.PutUint32(encoded, uint32(len(compressed))^part.mask)
			binary.LittleEndian.PutUint32(encoded[4:], uint32(len(part.raw)))
			encoded = append(encoded, compressed...)
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, encoded...)
	}
	// Read original NKPI data before changing the encoder's format.
	original, err := OpenBytes(buildSyntheticNKPIArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := original.FileRaw(0)
	if err != nil {
		t.Fatal(err)
	}
	body, err := a.encryptChunk(raw)
	if err != nil {
		t.Fatal(err)
	}
	items := []fileItem{a.items[0]}
	for _, name := range []string{"patch_old", "patch_new", "unused_str"} {
		off, ok := a.LookupStringMagic(name)
		if !ok {
			t.Fatal(name)
		}
		item := a.items[0]
		item.nameOffset = off
		if name == "unused_str" {
			item.pathOffset = -1
		}
		items = append(items, item)
	}
	items = append(items, items[0]) // duplicate: preserve first-path semantics
	head := a.header
	head.fileCount = len(items)
	head.hashSize = 0
	head.nameSize = len(names)
	head.bodySize = len(body)
	head.groupCount = 1
	output := a.encodeHeader(head)
	output = append(output, encodeFileTable(items)...)
	output = append(output, names...)
	output = append(output, a.encodeGroups([]groupItem{{len(body), len(raw)}})...)
	output = append(output, body...)
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.pvf")
	idx := filepath.Join(dir, "fixture.idx")
	if err = os.WriteFile(path, output, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(output)
	digest := hex.EncodeToString(sum[:])
	ref, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = BuildDiskIndex(context.Background(), path, digest, idx); err != nil {
		t.Fatal(err)
	}
	return path, idx, digest, ref
}

func TestDiskSourceMatchesBothFormatsAndCloses(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(map[bool]string{false: "nkpi", true: "protected"}[protected], func(t *testing.T) {
			path, idx, digest, ref := diskFixture(t, protected)
			d, err := OpenDisk(path, idx, digest)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if d.count != 4 || d.Snapshot().FileCount != 5 {
				t.Fatal("duplicate path semantics changed")
			}
			for _, file := range ref.files {
				want, err := ref.ReadText(file.ArchivePath)
				if err != nil {
					t.Fatal(err)
				}
				got, err := d.ReadText(strings.ToUpper(strings.ReplaceAll(file.ArchivePath, "/", "\\")))
				if err != nil || got != want {
					t.Fatalf("read differs: %s: %v", file.ArchivePath, err)
				}
				actual, ok := d.ResolvePath(file.ArchivePath)
				if !ok || actual != file.ArchivePath {
					t.Fatal("path differs")
				}
			}
			if _, err = d.ReadText("missing"); !errors.Is(err, ErrFileNotFound) {
				t.Fatalf("missing: %v", err)
			}
			if len(d.core.files) != 0 || len(d.core.pathIdx) != 0 || len(d.core.data) != 0 || len(d.core.items) != 0 {
				t.Fatal("full archive retained")
			}
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for n := 0; n < 50; n++ {
						if _, err := d.ReadText("dir/test.ani"); err != nil {
							t.Error(err)
						}
					}
				}()
			}
			wg.Wait()
			if err = d.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = d.ReadText("dir/test.ani"); !errors.Is(err, ErrDiskClosed) {
				t.Fatal(err)
			}
			if d.CacheStats() != (DiskStats{}) {
				t.Fatal("cache retained after close")
			}
		})
	}
}

func TestDiskIndexRejectsMismatchTruncationCorruptionAndOverwrite(t *testing.T) {
	path, idx, digest, _ := diskFixture(t, true)
	original, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err = BuildDiskIndex(context.Background(), path, digest, idx); err == nil {
		t.Fatal("existing index overwritten")
	}
	if after, _ := os.ReadFile(idx); !bytes.Equal(after, original) {
		t.Fatal("index changed")
	}
	if d, err := OpenDisk(path, idx, strings.Repeat("0", 64)); err == nil {
		d.Close()
		t.Fatal("wrong PVF accepted")
	}
	for _, change := range []func([]byte) []byte{
		func(raw []byte) []byte { return raw[:12] },
		func(raw []byte) []byte { raw[len(raw)-4] ^= 1; return raw },
		func(raw []byte) []byte { binary.LittleEndian.PutUint64(raw[8:], ^uint64(0)); return raw },
		func(raw []byte) []byte { raw[16] ^= 1; return raw },
	} {
		bad := change(append([]byte(nil), original...))
		target := filepath.Join(t.TempDir(), "bad.idx")
		if err = os.WriteFile(target, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if d, err := OpenDisk(path, target, digest); err == nil {
			d.Close()
			t.Fatal("invalid index accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	target := filepath.Join(t.TempDir(), "cancel.idx")
	if err = BuildDiskIndex(ctx, path, digest, target); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("cancelled index published")
	}
}

func TestDiskCachesBoundBytesEntriesAndKeepRecent(t *testing.T) {
	c := newBoundedCache[string, string](10)
	c.put("a", "12345", 5)
	c.put("b", "12345", 5)
	c.get("a")
	c.put("c", "12345", 5)
	if _, ok := c.get("b"); ok {
		t.Fatal("least-recent entry retained")
	}
	if _, ok := c.get("a"); !ok {
		t.Fatal("recent entry evicted")
	}
	c.put("large", strings.Repeat("x", 100), 100)
	if c.used > 10 {
		t.Fatal("byte budget exceeded")
	}
	zero := newBoundedCache[int, string](10)
	for i := 0; i < 10000; i++ {
		zero.put(i, "", 0)
	}
	if len(zero.entries) != diskMaxCacheEntries {
		t.Fatal("entry bound missing")
	}
	raw, err := compressZlib(bytes.Repeat([]byte{'x'}, 10000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = zlibBytesLimit(raw, 100); !errors.Is(err, ErrTooLarge) {
		t.Fatal("expansion bound missing")
	}
}

func TestDiskDirectoryPageEdgesAndEviction(t *testing.T) {
	const perPage = diskDirectoryPage / diskRecordSize
	const pages = diskDirectoryBudget/diskDirectoryPage + 7
	const count = pages*perPage + 3
	raw := make([]byte, diskHeaderSize+count*diskRecordSize)
	for i := 0; i < count; i++ {
		binary.LittleEndian.PutUint64(raw[diskHeaderSize+i*diskRecordSize:], uint64(i))
	}
	path := filepath.Join(t.TempDir(), "directory.idx")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d := &DiskArchive{index: f, count: count, directory: newBoundedCache[int64, []byte](diskDirectoryBudget)}
	check := func(position int64) {
		t.Helper()
		rec, err := d.readRecord(position)
		if err != nil || binary.LittleEndian.Uint64(rec[:]) != uint64(position) {
			t.Fatalf("record %d: %v", position, err)
		}
		if d.directory.used > diskDirectoryBudget {
			t.Fatal("directory cache exceeded budget")
		}
	}
	for _, position := range []int64{0, perPage - 1, perPage, count - 1} {
		check(position)
	}
	for i := 0; i <= pages; i++ {
		check(int64(i * perPage))
	}
	if _, ok := d.directory.entries[0]; ok {
		t.Fatal("cold page was not evicted")
	}
	check(0) // the evicted first page must still be readable from disk
	for _, position := range []int64{-1, count} {
		if _, err := d.readRecord(position); !errors.Is(err, ErrFileNotFound) {
			t.Fatal(err)
		}
	}
}

func TestBuildRealDiskIndex(t *testing.T) {
	path, idx, digest := os.Getenv("GM_DISK_PVF"), os.Getenv("GM_DISK_INDEX"), os.Getenv("GM_DISK_SHA256")
	if path == "" || idx == "" || digest == "" {
		t.Skip("set isolated GM_DISK_PVF, GM_DISK_INDEX, GM_DISK_SHA256")
	}
	if err := BuildDiskIndex(context.Background(), path, digest, idx); err != nil {
		t.Fatal(err)
	}
}

func TestRealDiskTextParity(t *testing.T) {
	path, idx, digest := os.Getenv("GM_DISK_PVF"), os.Getenv("GM_DISK_INDEX"), os.Getenv("GM_DISK_SHA256")
	if path == "" || idx == "" || digest == "" {
		t.Skip("set isolated GM_DISK_PVF, GM_DISK_INDEX, GM_DISK_SHA256")
	}
	d, err := OpenDisk(path, idx, digest)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ref, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for i, file := range ref.files {
		if i%997 != 0 {
			continue
		}
		want, err := ref.ReadText(file.ArchivePath)
		if err != nil {
			t.Fatal(err)
		}
		got, err := d.ReadText(file.ArchivePath)
		if err != nil || got != want {
			t.Fatalf("sample %d differs: %v", i, err)
		}
		count++
	}
	for _, path := range []string{"stackable/stackable.lst", "equipment/equipment.lst", "n_quest/quest.lst", "skill/skilllist.lst"} {
		want, err := ref.ReadText(path)
		if err != nil {
			continue
		}
		got, err := d.ReadText(path)
		if err != nil || got != want {
			t.Fatal("list differs", path, err)
		}
	}
	stats := d.CacheStats()
	if stats.ChunkBytes > diskChunkBudget || stats.TextBytes > diskTextBudget || stats.ChunkEntries > diskMaxCacheEntries || stats.TextEntries > diskMaxCacheEntries || stats.DirectoryBytes > diskDirectoryBudget || stats.DirectoryPages > diskDirectoryBudget/diskDirectoryPage {
		t.Fatal(stats)
	}
	t.Logf("%d distributed real-file samples match; cache %+v", count, stats)
}
