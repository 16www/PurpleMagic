package gmtool

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	dnfquest "dnf90.local/gateway/internal/modules/dnf/quest"
)

const questIndexVersion = 1
const questIndexHeader = 128
const maxQuestDefinition = 2 << 20

type questIndexRow struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Grade    string `json:"grade"`
	LevelMin int    `json:"levelMin"`
	LevelMax int    `json:"levelMax"`
	Offset   int64  `json:"offset"`
	Size     int    `json:"size"`
	SHA256   string `json:"sha256"`
}
type questDiskIndex struct {
	Path         string
	Checksum     string
	ManifestHash string
	Rows         []questIndexRow
}
type QuestIndexReport struct {
	Path        string `json:"path"`
	PVFChecksum string `json:"pvfChecksum"`
	Quests      int    `json:"quests"`
	Reused      bool   `json:"reused"`
}

func questIndexPath(summary, checksum string) string {
	if len(checksum) != 64 || summary == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(summary), fmt.Sprintf("quests-v%d-%s.bin", questIndexVersion, checksum))
}
func openQuestIndex(path, checksum string) (*os.File, []byte, error) {
	if path == "" {
		return nil, nil, os.ErrNotExist
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*os.File, []byte, error) { f.Close(); return nil, nil, err }
	h := make([]byte, questIndexHeader)
	if _, err = io.ReadFull(f, h); err != nil {
		return fail(err)
	}
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	offset, size := binary.LittleEndian.Uint64(h[48:56]), binary.LittleEndian.Uint64(h[56:64])
	if string(h[:8]) != "GMQUEST1" || hex.EncodeToString(h[8:40]) != checksum || binary.LittleEndian.Uint64(h[40:48]) != questIndexVersion || offset < questIndexHeader || size > 16<<20 || offset > uint64(info.Size()) || size != uint64(info.Size())-offset {
		return fail(errors.New("quest index identity or bounds mismatch"))
	}
	return f, h, nil
}
func readQuestIndex(ctx context.Context, path, checksum string) (*questDiskIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, h, err := openQuestIndex(path, checksum)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	offset, size := int64(binary.LittleEndian.Uint64(h[48:56])), int(binary.LittleEndian.Uint64(h[56:64]))
	raw := make([]byte, size)
	if _, err = f.ReadAt(raw, offset); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if !bytes.Equal(sum[:], h[64:96]) {
		return nil, errors.New("quest manifest checksum mismatch")
	}
	ix := &questDiskIndex{Path: path, Checksum: checksum, ManifestHash: hex.EncodeToString(sum[:])}
	if err = json.Unmarshal(raw, &ix.Rows); err != nil {
		return nil, err
	}
	if len(ix.Rows) == 0 || len(ix.Rows) > 100000 {
		return nil, errors.New("quest index count invalid")
	}
	last := int64(0)
	end := int64(questIndexHeader)
	for _, r := range ix.Rows {
		digest, e := hex.DecodeString(r.SHA256)
		if r.ID <= last || r.ID > 2147483647 || r.Offset != end || r.Size < 1 || r.Size > maxQuestDefinition || r.Offset > offset-int64(r.Size) || e != nil || len(digest) != 32 {
			return nil, errors.New("quest index row invalid")
		}
		last = r.ID
		end = r.Offset + int64(r.Size)
	}
	if end != offset {
		return nil, errors.New("quest index payload bounds mismatch")
	}
	return ix, ctx.Err()
}
func (ix *questDiskIndex) find(id int64) (questIndexRow, bool) {
	i := sort.Search(len(ix.Rows), func(i int) bool { return ix.Rows[i].ID >= id })
	if i < len(ix.Rows) && ix.Rows[i].ID == id {
		return ix.Rows[i], true
	}
	return questIndexRow{}, false
}
func (ix *questDiskIndex) definition(ctx context.Context, id int64) (dnfquest.Definition, error) {
	var d dnfquest.Definition
	if err := ctx.Err(); err != nil {
		return d, err
	}
	row, ok := ix.find(id)
	if !ok {
		return d, os.ErrNotExist
	}
	f, h, err := openQuestIndex(ix.Path, ix.Checksum)
	if err != nil {
		return d, err
	}
	defer f.Close()
	if hex.EncodeToString(h[64:96]) != ix.ManifestHash {
		return d, errors.New("quest index changed during read")
	}
	raw := make([]byte, row.Size)
	if _, err = f.ReadAt(raw, row.Offset); err != nil {
		return d, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != row.SHA256 {
		return d, errors.New("quest record checksum mismatch")
	}
	z, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return d, err
	}
	decoded, err := io.ReadAll(io.LimitReader(z, maxQuestDefinition+1))
	z.Close()
	if err != nil {
		return d, err
	}
	if len(decoded) > maxQuestDefinition {
		return d, errors.New("quest record too large")
	}
	if err = json.Unmarshal(decoded, &d); err != nil {
		return d, err
	}
	if d.ID != id || d.Path != row.Path || d.Name != row.Name {
		return d, errors.New("quest record identity mismatch")
	}
	return d, ctx.Err()
}

// BuildQuestIndex is offline. Only compact summaries stay in the running GM;
// description, conditions and rewards are decompressed one record at a time.
func BuildQuestIndex(ctx context.Context, source dnfpvf.Source, summary string) (QuestIndexReport, error) {
	checksum := itemIndexSourceChecksum(source)
	report := QuestIndexReport{Path: questIndexPath(summary, checksum), PVFChecksum: checksum}
	if report.Path == "" {
		return report, errors.New("quest index requires PVF SHA256 and summary path")
	}
	if ix, err := readQuestIndex(ctx, report.Path, checksum); err == nil {
		valid := true
		for _, r := range ix.Rows {
			if _, err = ix.definition(ctx, r.ID); err != nil {
				valid = false
				break
			}
		}
		if valid {
			report.Quests = len(ix.Rows)
			report.Reused = true
			return report, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	index, err := dnfpvf.Build(ctx, source, dnfpvf.BuildOptions{Lists: []string{dnfquest.DefaultList}})
	if err != nil {
		return report, err
	}
	catalog, err := dnfquest.Load(ctx, index)
	if err != nil {
		return report, err
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, entry := range index.List(dnfquest.DefaultList) {
		if _, ok := catalog.Find(entry.ID); ok && !seen[entry.ID] {
			ids = append(ids, entry.ID)
			seen[entry.ID] = true
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if err = os.MkdirAll(filepath.Dir(report.Path), 0700); err != nil {
		return report, err
	}
	f, err := os.CreateTemp(filepath.Dir(report.Path), ".quest-index-*")
	if err != nil {
		return report, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	h := make([]byte, questIndexHeader)
	if _, err = f.Write(h); err != nil {
		return report, err
	}
	offset := int64(questIndexHeader)
	rows := []questIndexRow{}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		d, _ := catalog.Find(id)
		raw, e := json.Marshal(d)
		if e != nil {
			return report, e
		}
		if len(raw) > maxQuestDefinition {
			return report, errors.New("quest definition too large")
		}
		var buf bytes.Buffer
		z := zlib.NewWriter(&buf)
		if _, e = z.Write(raw); e != nil {
			return report, e
		}
		if e = z.Close(); e != nil {
			return report, e
		}
		if buf.Len() > maxQuestDefinition {
			return report, errors.New("compressed quest too large")
		}
		raw = buf.Bytes()
		sum := sha256.Sum256(raw)
		if _, err = f.Write(raw); err != nil {
			return report, err
		}
		rows = append(rows, questIndexRow{id, d.Name, d.Path, d.Grade, d.LevelMin, d.LevelMax, offset, len(raw), hex.EncodeToString(sum[:])})
		offset += int64(len(raw))
	}
	manifest, err := json.Marshal(rows)
	if err != nil {
		return report, err
	}
	if len(manifest) > 16<<20 {
		return report, errors.New("quest manifest too large")
	}
	if _, err = f.Write(manifest); err != nil {
		return report, err
	}
	copy(h[:8], "GMQUEST1")
	digest, err := hex.DecodeString(checksum)
	if err != nil {
		return report, err
	}
	copy(h[8:40], digest)
	binary.LittleEndian.PutUint64(h[40:48], questIndexVersion)
	binary.LittleEndian.PutUint64(h[48:56], uint64(offset))
	binary.LittleEndian.PutUint64(h[56:64], uint64(len(manifest)))
	sum := sha256.Sum256(manifest)
	copy(h[64:96], sum[:])
	if _, err = f.WriteAt(h, 0); err != nil {
		return report, err
	}
	if err = f.Sync(); err != nil {
		return report, err
	}
	if err = f.Close(); err != nil {
		return report, err
	}
	ix, err := readQuestIndex(ctx, tmp, checksum)
	if err != nil {
		return report, err
	}
	for _, r := range ix.Rows {
		if _, err = ix.definition(ctx, r.ID); err != nil {
			return report, err
		}
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if err = os.Rename(tmp, report.Path); err != nil {
		return report, err
	}
	report.Quests = len(rows)
	return report, nil
}

func (s *Server) questReadIndex(ctx context.Context) (*questDiskIndex, error) {
	if s.deps.PVF == nil || s.items == nil {
		return nil, errors.New("PVF unavailable")
	}
	source, err := s.source()
	if err != nil {
		return nil, err
	}
	checksum := itemIndexSourceChecksum(source)
	s.questIndexMu.Lock()
	defer s.questIndexMu.Unlock()
	if s.questIndex != nil && s.questIndex.Checksum == checksum {
		return s.questIndex, ctx.Err()
	}
	ix, err := readQuestIndex(ctx, questIndexPath(s.items.cachePath, checksum), checksum)
	if err != nil {
		return nil, err
	}
	s.questIndex = ix
	return ix, nil
}
