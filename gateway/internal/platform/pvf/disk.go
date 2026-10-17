package pvf

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The on-disk directory stores fixed-size SHA256(path) records. Startup and
// lookup never materialize the million-entry File slice/path map in the server.
// The original PVF remains immutable; reads share the existing text decoder.
const (
	diskMagic           = "GMPVF001"
	diskHeaderSize      = 128
	diskRecordSize      = 64
	diskChunkBudget     = 8 << 20
	diskTextBudget      = 2 << 20
	diskDirectoryBudget = 4 << 20
	diskDirectoryPage   = 16 << 10
	diskMaxCacheEntries = 512
	diskMaxChunk        = 64 << 20
)

var ErrDiskClosed = errors.New("PVF disk source is closed")

type diskRecord [diskRecordSize]byte

// BuildDiskIndex is an offline operation. It uses the existing archive parser,
// writes a new immutable index, and refuses to replace an existing target.
// The caller should exit after building, releasing the importer's large heap.
func BuildDiskIndex(ctx context.Context, pvfPath, expectedSHA, target string) error {
	if ctx == nil {
		return errors.New("PVF index requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	expected, err := parseDigest(expectedSHA)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(target); err == nil {
		return errors.New("PVF index already exists; choose a new target")
	} else if !os.IsNotExist(err) {
		return err
	}
	a, err := Open(pvfPath)
	if err != nil {
		return err
	}
	if a.snapshot.Checksum != hex.EncodeToString(expected[:]) {
		return errors.New("PVF index source SHA256 mismatch")
	}
	records := make([]diskRecord, 0, len(a.pathIdx))
	for key, idx := range a.pathIdx {
		if err = ctx.Err(); err != nil {
			return err
		}
		item := a.items[idx]
		if idx < 0 || uint64(idx) > uint64(^uint32(0)) {
			return fmt.Errorf("%w: invalid indexed file %d: %+v", ErrInvalidArchive, idx, item)
		}
		var rec diskRecord
		sum := sha256.Sum256([]byte(key))
		copy(rec[:32], sum[:])
		for n, value := range []int{idx, item.chunkIndex, item.dataOffset, item.dataSize, item.dataType, item.nameOffset, item.pathOffset} {
			binary.LittleEndian.PutUint32(rec[32+n*4:], uint32(value))
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool { return bytes.Compare(records[i][:32], records[j][:32]) < 0 })
	for i := 1; i < len(records); i++ {
		if bytes.Equal(records[i-1][:32], records[i][:32]) {
			return errors.New("PVF index path digest collision")
		}
	}
	if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".pvf-index-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	header := make([]byte, diskHeaderSize)
	copy(header, diskMagic)
	binary.LittleEndian.PutUint64(header[8:16], uint64(len(records)))
	binary.LittleEndian.PutUint64(header[16:24], uint64(len(a.files)))
	binary.LittleEndian.PutUint64(header[24:32], uint64(a.snapshot.Size))
	copy(header[32:64], expected[:])
	hash := sha256.New()
	// Bind metadata as well as table bytes. Bytes 64..95 contain this digest.
	hash.Write(header[:64])
	hash.Write(header[96:])
	if _, err = f.Write(header); err != nil {
		return err
	}
	writer := bufio.NewWriterSize(f, 1<<20)
	for i := range records {
		if i%4096 == 0 {
			if err = ctx.Err(); err != nil {
				return err
			}
		}
		if _, err = writer.Write(records[i][:]); err != nil {
			return err
		}
		hash.Write(records[i][:])
	}
	if err = writer.Flush(); err != nil {
		return err
	}
	copy(header[64:96], hash.Sum(nil))
	if _, err = f.WriteAt(header, 0); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// Same-directory hard link atomically publishes only when target is absent.
	// It avoids rename's overwrite behavior and concurrent importer races.
	if err = os.Link(tmp, target); err != nil {
		return fmt.Errorf("publish new PVF index: %w", err)
	}
	return nil
}

type boundedEntry[K comparable, V any] struct {
	key   K
	value V
	bytes int
}
type boundedCache[K comparable, V any] struct {
	budget, used int
	entries      map[K]*list.Element
	order        *list.List
}

func newBoundedCache[K comparable, V any](budget int) *boundedCache[K, V] {
	return &boundedCache[K, V]{budget: budget, entries: make(map[K]*list.Element), order: list.New()}
}
func (c *boundedCache[K, V]) get(key K) (V, bool) {
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(boundedEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}
func (c *boundedCache[K, V]) put(key K, value V, size int) {
	if size > c.budget || size < 0 {
		return
	}
	if old, ok := c.entries[key]; ok {
		c.used -= old.Value.(boundedEntry[K, V]).bytes
		c.order.Remove(old)
		delete(c.entries, key)
	}
	for c.used+size > c.budget || len(c.entries) >= diskMaxCacheEntries {
		el := c.order.Back()
		e := el.Value.(boundedEntry[K, V])
		c.used -= e.bytes
		delete(c.entries, e.key)
		c.order.Remove(el)
	}
	c.entries[key] = c.order.PushFront(boundedEntry[K, V]{key, value, size})
	c.used += size
}

// DiskArchive keeps only the shared string pools and group table in memory.
// Directory/chunk/text caches have both byte and entry limits. A mutex also
// bounds decompression work during concurrent misses and coordinates Close.
type DiskArchive struct {
	mu         sync.Mutex
	core       *Archive
	pvf, index *os.File
	count      int64
	chunks     *boundedCache[int, []byte]
	texts      *boundedCache[string, string]
	directory  *boundedCache[int64, []byte]
}

type DiskStats struct{ ChunkBytes, TextBytes, ChunkEntries, TextEntries, DirectoryBytes, DirectoryPages int }

func parseDigest(value string) ([32]byte, error) {
	var sum [32]byte
	raw, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) != len(sum) {
		return sum, errors.New("PVF SHA256 must have 64 hex characters")
	}
	copy(sum[:], raw)
	return sum, nil
}

func OpenDisk(pvfPath, indexPath, expectedSHA string) (*DiskArchive, error) {
	expected, err := parseDigest(expectedSHA)
	if err != nil {
		return nil, err
	}
	pvf, err := os.Open(pvfPath)
	if err != nil {
		return nil, err
	}
	d := &DiskArchive{pvf: pvf, chunks: newBoundedCache[int, []byte](diskChunkBudget), texts: newBoundedCache[string, string](diskTextBudget), directory: newBoundedCache[int64, []byte](diskDirectoryBudget)}
	ok := false
	defer func() {
		if !ok {
			d.Close()
		}
	}()
	info, err := pvf.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < headerSize || info.Size() > DefaultMaxBytes {
		return nil, ErrInvalidArchive
	}
	h := sha256.New()
	if _, err = io.Copy(h, pvf); err != nil {
		return nil, err
	}
	if !bytes.Equal(h.Sum(nil), expected[:]) {
		return nil, errors.New("PVF disk source SHA256 mismatch")
	}
	d.index, err = os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("PVF disk index unavailable; build it offline first: %w", err)
	}
	ii, err := d.index.Stat()
	if err != nil {
		return nil, err
	}
	header := make([]byte, diskHeaderSize)
	if _, err = d.index.ReadAt(header, 0); err != nil {
		return nil, ErrInvalidArchive
	}
	count := binary.LittleEndian.Uint64(header[8:16])
	if string(header[:8]) != diskMagic || count > uint64(DefaultMaxBytes/fileItemSize) || !ii.Mode().IsRegular() || ii.Size() != diskHeaderSize+int64(count)*diskRecordSize || binary.LittleEndian.Uint64(header[24:32]) != uint64(info.Size()) || !bytes.Equal(header[32:64], expected[:]) {
		return nil, errors.New("PVF disk index header/source mismatch")
	}
	h.Reset()
	h.Write(header[:64])
	h.Write(header[96:])
	if _, err = io.Copy(h, io.NewSectionReader(d.index, diskHeaderSize, ii.Size()-diskHeaderSize)); err != nil {
		return nil, err
	}
	if !bytes.Equal(h.Sum(nil), header[64:96]) {
		return nil, errors.New("PVF disk index checksum mismatch")
	}
	d.count = int64(count)
	d.core = &Archive{snapshot: Snapshot{Path: pvfPath, Size: info.Size(), Checksum: hex.EncodeToString(expected[:]), LoadedAt: time.Now().UTC()}}
	if err = d.loadTables(); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint64(header[16:24]) != uint64(d.core.header.fileCount) || d.count > int64(d.core.header.fileCount) {
		return nil, ErrInvalidArchive
	}
	ok = true
	return d, nil
}

func (d *DiskArchive) loadTables() error {
	header := make([]byte, headerSize)
	if _, err := d.pvf.ReadAt(header, 0); err != nil {
		return err
	}
	plain := append([]byte(nil), header...)
	decryptGuard(plain)
	decrypt("HeaD", plain)
	protected := false
	if binary.LittleEndian.Uint32(plain[:4]) != magicSignature {
		copy(plain, header)
		decryptProtected("hEAd", plain)
		protected = true
	}
	if binary.LittleEndian.Uint32(plain[:4]) != magicSignature {
		return ErrInvalidArchive
	}
	h := parseHeaderSizes(plain)
	if err := validateHeader(h); err != nil {
		return err
	}
	nameOff := int64(headerSize) + int64(h.fileCount)*fileItemSize + int64(h.hashSize)
	groupOff := nameOff + int64(h.nameSize)
	bodyOff := groupOff + int64(h.groupCount)*groupItemSize
	if nameOff < headerSize || bodyOff > d.core.snapshot.Size || int64(h.bodySize) > d.core.snapshot.Size-bodyOff {
		return ErrInvalidArchive
	}
	d.core.header = h
	d.core.bodyOff = int(bodyOff)
	names := make([]byte, h.nameSize)
	if _, err := d.pvf.ReadAt(names, nameOff); err != nil {
		return err
	}
	if protected {
		if err := d.core.buildPStringBuffers(names); err != nil {
			return err
		}
		d.core.format = FormatProtectedNKPI
	} else {
		d.core.buildStringBuffers(names)
		d.core.format = FormatNKPI
	}
	groups := make([]byte, h.groupCount*groupItemSize)
	if _, err := d.pvf.ReadAt(groups, groupOff); err != nil {
		return err
	}
	if protected {
		decryptProtected("grpi", groups)
	} else {
		decrypt("GRPI", groups)
	}
	if err := d.core.parseGroups(groups, h.groupCount); err != nil {
		return err
	}
	prev := 0
	for _, group := range d.core.groups {
		if group.compressedSize <= prev || group.compressedSize > h.bodySize || group.originalSize > diskMaxChunk {
			return ErrInvalidArchive
		}
		prev = group.compressedSize
	}
	return nil
}

func (d *DiskArchive) record(key string) (diskRecord, error) {
	var rec diskRecord
	if d.core == nil || d.index == nil {
		return rec, ErrDiskClosed
	}
	want := sha256.Sum256([]byte(key))
	lo, hi := int64(0), d.count
	for lo < hi {
		mid := lo + (hi-lo)/2
		var err error
		rec, err = d.readRecord(mid)
		if err != nil {
			return rec, err
		}
		cmp := bytes.Compare(rec[:32], want[:])
		if cmp < 0 {
			lo = mid + 1
		} else if cmp > 0 {
			hi = mid
		} else {
			return rec, nil
		}
	}
	return rec, ErrFileNotFound
}

// Cache bounded pages, not individual paths. Binary searches share upper tree
// pages and adjacent records, avoiding one OS read for every comparison.
func (d *DiskArchive) readRecord(position int64) (diskRecord, error) {
	var record diskRecord
	if position < 0 || position >= d.count {
		return record, ErrFileNotFound
	}
	const perPage = diskDirectoryPage / diskRecordSize
	number := position / perPage
	page, ok := d.directory.get(number)
	if !ok {
		size := min(int64(diskDirectoryPage), (d.count-number*perPage)*diskRecordSize)
		page = make([]byte, size)
		if _, err := d.index.ReadAt(page, diskHeaderSize+number*diskDirectoryPage); err != nil {
			return record, err
		}
		d.directory.put(number, page, cap(page))
	}
	offset := position % perPage * diskRecordSize
	copy(record[:], page[offset:offset+diskRecordSize])
	return record, nil
}

func recordItem(rec diskRecord) fileItem {
	// Preserve signed sentinels such as pathOffset=-1 (a root-level file).
	return fileItem{chunkIndex: readInt32(rec[36:40]), dataOffset: readInt32(rec[40:44]), dataSize: readInt32(rec[44:48]), dataType: readInt32(rec[48:52]), nameOffset: readInt32(rec[52:56]), pathOffset: readInt32(rec[56:60])}
}

// ResolvePath provides list-reference existence checks without Files()'s huge
// full-directory slice. It returns the archive's original case/path spelling.
func (d *DiskArchive) ResolvePath(value string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := pathKey(value)
	rec, err := d.record(key)
	if err != nil {
		return "", false
	}
	item := recordItem(rec)
	actual := joinArchivePath(d.core.resolveString(item.pathOffset), d.core.resolveString(item.nameOffset))
	return actual, pathKey(actual) == key
}

func (d *DiskArchive) ReadText(value string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.core == nil {
		return "", ErrDiskClosed
	}
	key := pathKey(value)
	if text, ok := d.texts.get(key); ok {
		return text, nil
	}
	rec, err := d.record(key)
	if err != nil {
		return "", err
	}
	item := recordItem(rec)
	actual := joinArchivePath(d.core.resolveString(item.pathOffset), d.core.resolveString(item.nameOffset))
	if pathKey(actual) != key {
		return "", ErrInvalidArchive
	}
	chunk, err := d.chunk(item.chunkIndex)
	if err != nil {
		return "", err
	}
	if item.dataOffset < 0 || item.dataSize < 0 || item.dataOffset > len(chunk) || item.dataSize > len(chunk)-item.dataOffset {
		return "", ErrInvalidArchive
	}
	text := d.core.decodeText(item.dataType, chunk[item.dataOffset:item.dataOffset+item.dataSize])
	// Builder strings and io.ReadAll buffers can have excess backing capacity.
	// Clone cached text and account for chunk capacity to bound retained bytes.
	text = strings.Clone(text)
	d.texts.put(key, text, len(key)+len(text))
	return text, nil
}

func (d *DiskArchive) chunk(idx int) ([]byte, error) {
	if chunk, ok := d.chunks.get(idx); ok {
		return chunk, nil
	}
	if idx < 0 || idx >= len(d.core.groups) {
		return nil, ErrInvalidArchive
	}
	prev := 0
	if idx > 0 {
		prev = d.core.groups[idx-1].compressedSize
	}
	group := d.core.groups[idx]
	size := group.compressedSize - prev
	if size <= 0 || size > diskMaxChunk || group.originalSize > diskMaxChunk {
		return nil, ErrInvalidArchive
	}
	encrypted := make([]byte, size)
	if _, err := d.pvf.ReadAt(encrypted, int64(d.core.bodyOff+prev)); err != nil {
		return nil, err
	}
	if d.core.format == FormatProtectedNKPI {
		decryptProtected("bODy", encrypted)
	} else {
		decrypt("BodY", encrypted)
	}
	chunk, err := zlibBytesLimit(encrypted, int64(group.originalSize))
	if err != nil {
		return nil, err
	}
	if len(chunk) != group.originalSize {
		return nil, ErrInvalidArchive
	}
	d.chunks.put(idx, chunk, cap(chunk))
	return chunk, nil
}

func (d *DiskArchive) Snapshot() ArchiveSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.core == nil {
		return ArchiveSnapshot{}
	}
	a := d.core
	return ArchiveSnapshot{Format: a.format, Path: a.snapshot.Path, Size: a.snapshot.Size, Checksum: a.snapshot.Checksum, LoadedAt: a.snapshot.LoadedAt.Format(rfc3339Nano), FileCount: a.header.fileCount, GroupCount: len(a.groups), CachedChunks: len(d.chunks.entries), CachedTexts: len(d.texts.entries)}
}
func (d *DiskArchive) CacheStats() DiskStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return DiskStats{ChunkBytes: d.chunks.used, TextBytes: d.texts.used, ChunkEntries: len(d.chunks.entries), TextEntries: len(d.texts.entries), DirectoryBytes: d.directory.used, DirectoryPages: len(d.directory.entries)}
}
func (d *DiskArchive) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var err error
	if d.index != nil {
		err = d.index.Close()
		d.index = nil
	}
	if d.pvf != nil {
		err = errors.Join(err, d.pvf.Close())
		d.pvf = nil
	}
	d.core = nil
	d.chunks = newBoundedCache[int, []byte](diskChunkBudget)
	d.texts = newBoundedCache[string, string](diskTextBudget)
	d.directory = newBoundedCache[int64, []byte](diskDirectoryBudget)
	return err
}
