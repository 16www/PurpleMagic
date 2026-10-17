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
	"reflect"
	"sort"

	"dnf90.local/gateway/internal/modules/dnf/profession"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/quest"
	"dnf90.local/gateway/internal/modules/dnf/skill"
)

const professionIndexVersion = 1
const professionIndexHeader = 128
const professionRecordLimit = 16 << 20

type professionIndexRecord struct {
	Offset int64  `json:"offset"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}
type professionIndexJob struct {
	Job     byte                  `json:"job"`
	Name    string                `json:"name"`
	Choices []profession.Choice   `json:"choices"`
	Record  professionIndexRecord `json:"record"`
}
type professionIndexManifest struct {
	Jobs   []professionIndexJob  `json:"jobs"`
	Quests professionIndexRecord `json:"quests"`
}
type professionDiskIndex struct {
	Path, Checksum, ManifestHash string
	Manifest                     professionIndexManifest
}
type professionJobPayload struct {
	Job     byte          `json:"job"`
	Profile string        `json:"profile"`
	Skills  []skill.Skill `json:"skills"`
}
type professionJobResources struct {
	Profiles *profession.Profiles
	Skills   *skill.Table
}
type ProfessionIndexReport struct {
	Path        string `json:"path"`
	PVFChecksum string `json:"pvfChecksum"`
	Jobs        int    `json:"jobs"`
	Skills      int    `json:"skills"`
	Quests      int    `json:"quests"`
	Bytes       int64  `json:"bytes"`
	Reused      bool   `json:"reused"`
}

func professionIndexPath(summary, hash string) string {
	digest, e := hex.DecodeString(hash)
	if e != nil || len(digest) != 32 || summary == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(summary), fmt.Sprintf("professions-v%d-%s.bin", professionIndexVersion, hash))
}
func openProfessionIndex(path, hash string) (*os.File, []byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, nil, e
	}
	fail := func(e error) (*os.File, []byte, error) { f.Close(); return nil, nil, e }
	h := make([]byte, professionIndexHeader)
	if _, e = io.ReadFull(f, h); e != nil {
		return fail(e)
	}
	info, e := f.Stat()
	if e != nil {
		return fail(e)
	}
	offset, size := binary.LittleEndian.Uint64(h[48:56]), binary.LittleEndian.Uint64(h[56:64])
	if string(h[:8]) != "GMPROF01" || hex.EncodeToString(h[8:40]) != hash || binary.LittleEndian.Uint64(h[40:48]) != professionIndexVersion || offset < professionIndexHeader || size == 0 || size > 1<<20 || offset > uint64(info.Size()) || size != uint64(info.Size())-offset {
		return fail(errors.New("profession index identity or bounds mismatch"))
	}
	return f, h, nil
}
func readProfessionIndex(ctx context.Context, path, hash string) (*professionDiskIndex, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	f, h, e := openProfessionIndex(path, hash)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	offset, size := int64(binary.LittleEndian.Uint64(h[48:56])), int(binary.LittleEndian.Uint64(h[56:64]))
	raw := make([]byte, size)
	if _, e = f.ReadAt(raw, offset); e != nil {
		return nil, e
	}
	digest := sha256.Sum256(raw)
	if !bytes.Equal(digest[:], h[64:96]) {
		return nil, errors.New("profession manifest checksum mismatch")
	}
	ix := &professionDiskIndex{Path: path, Checksum: hash, ManifestHash: hex.EncodeToString(digest[:])}
	if e = json.Unmarshal(raw, &ix.Manifest); e != nil {
		return nil, e
	}
	if len(ix.Manifest.Jobs) == 0 || len(ix.Manifest.Jobs) > 256 {
		return nil, errors.New("profession job count invalid")
	}
	end := int64(professionIndexHeader)
	last := -1
	validRecord := func(r professionIndexRecord) bool {
		d, e := hex.DecodeString(r.SHA256)
		if e != nil || len(d) != 32 || r.Offset != end || r.Size < 1 || r.Size > professionRecordLimit || r.Offset > offset-int64(r.Size) {
			return false
		}
		end += int64(r.Size)
		return true
	}
	for _, j := range ix.Manifest.Jobs {
		if int(j.Job) <= last || j.Name == "" || len(j.Choices) == 0 || len(j.Choices) > 48 || !validRecord(j.Record) {
			return nil, errors.New("profession job record invalid")
		}
		last = int(j.Job)
		previous := -1
		for _, c := range j.Choices {
			first, stage := profession.Decode(c.GrowType)
			minimum := 1
			if first > 0 {
				minimum = 15
			}
			if stage == 1 {
				minimum = 50
			}
			if stage == 2 {
				minimum = 75
			}
			if int(c.GrowType) <= previous || stage > 2 || c.Name == "" || c.MinimumLevel != minimum {
				return nil, errors.New("profession choice invalid")
			}
			previous = int(c.GrowType)
		}
	}
	if !validRecord(ix.Manifest.Quests) || end != offset {
		return nil, errors.New("profession payload bounds mismatch")
	}
	return ix, ctx.Err()
}
func (ix *professionDiskIndex) record(ctx context.Context, r professionIndexRecord, target any) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	f, h, e := openProfessionIndex(ix.Path, ix.Checksum)
	if e != nil {
		return e
	}
	defer f.Close()
	if hex.EncodeToString(h[64:96]) != ix.ManifestHash {
		return errors.New("profession index changed during read")
	}
	raw := make([]byte, r.Size)
	if _, e = f.ReadAt(raw, r.Offset); e != nil {
		return e
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != r.SHA256 {
		return errors.New("profession record checksum mismatch")
	}
	z, e := zlib.NewReader(bytes.NewReader(raw))
	if e != nil {
		return e
	}
	decoded, e := io.ReadAll(io.LimitReader(z, professionRecordLimit+1))
	z.Close()
	if e != nil {
		return e
	}
	if len(decoded) > professionRecordLimit {
		return errors.New("profession record too large")
	}
	if e = json.Unmarshal(decoded, target); e != nil {
		return e
	}
	return ctx.Err()
}
func (ix *professionDiskIndex) job(ctx context.Context, id byte) (professionJobResources, error) {
	for _, j := range ix.Manifest.Jobs {
		if j.Job != id {
			continue
		}
		var p professionJobPayload
		if e := ix.record(ctx, j.Record, &p); e != nil {
			return professionJobResources{}, e
		}
		if p.Job != id {
			return professionJobResources{}, errors.New("profession job identity mismatch")
		}
		profiles, e := profession.ProfilesFromText(id, p.Profile)
		if e != nil {
			return professionJobResources{}, e
		}
		if name, _ := profiles.DisplayName(id, 0); name != j.Name || !reflect.DeepEqual(profiles.Choices(id), j.Choices) {
			return professionJobResources{}, errors.New("profession profile mismatch")
		}
		table, e := skill.TableFromDefinitions(id, p.Skills)
		if e != nil {
			return professionJobResources{}, e
		}
		return professionJobResources{Profiles: profiles, Skills: table}, nil
	}
	return professionJobResources{}, profession.ErrProfileMissing
}
func (ix *professionDiskIndex) quests(ctx context.Context) (*quest.Catalog, error) {
	var definitions []quest.Definition
	if e := ix.record(ctx, ix.Manifest.Quests, &definitions); e != nil {
		return nil, e
	}
	if len(definitions) == 0 || len(definitions) > 4096 {
		return nil, errors.New("profession quest count invalid")
	}
	for _, d := range definitions {
		if d.JobChangeQuest < 1 || d.JobChangeQuest > 3 {
			return nil, errors.New("non-profession quest in index")
		}
	}
	return quest.CatalogFromDefinitions(definitions)
}

func validateProfessionIndex(ctx context.Context, ix *professionDiskIndex) (ProfessionIndexReport, error) {
	r := ProfessionIndexReport{Path: ix.Path, PVFChecksum: ix.Checksum, Jobs: len(ix.Manifest.Jobs)}
	for _, j := range ix.Manifest.Jobs {
		v, e := ix.job(ctx, j.Job)
		if e != nil {
			return r, e
		}
		r.Skills += v.Skills.Snapshot().Skills
	}
	q, e := ix.quests(ctx)
	if e != nil {
		return r, e
	}
	r.Quests = q.Snapshot().Definitions
	info, e := os.Stat(ix.Path)
	if e != nil {
		return r, e
	}
	r.Bytes = info.Size()
	return r, ctx.Err()
}

// BuildProfessionIndex runs offline. Only the manifest, selected job and the
// profession quest subset are needed by a running gateway. The importer exits
// after atomically installing a fully read-back-verified generation.
func BuildProfessionIndex(ctx context.Context, source dnfpvf.Source, summary string) (ProfessionIndexReport, error) {
	hash := itemIndexSourceChecksum(source)
	report := ProfessionIndexReport{Path: professionIndexPath(summary, hash), PVFChecksum: hash}
	if report.Path == "" {
		return report, errors.New("profession index requires PVF SHA256 and summary path")
	}
	if ix, e := readProfessionIndex(ctx, report.Path, hash); e == nil {
		if verified, e := validateProfessionIndex(ctx, ix); e == nil {
			verified.Reused = true
			return verified, nil
		}
	}
	if e := ctx.Err(); e != nil {
		return report, e
	}
	index, e := dnfpvf.Build(ctx, source, dnfpvf.BuildOptions{Lists: []string{skill.DefaultList, profession.DefaultCharacterList}})
	if e != nil {
		return report, e
	}
	table, e := skill.Load(ctx, index, skill.Options{})
	if e != nil {
		return report, e
	}
	entries := index.List(profession.DefaultCharacterList)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	if e = os.MkdirAll(filepath.Dir(report.Path), 0700); e != nil {
		return report, e
	}
	f, e := os.CreateTemp(filepath.Dir(report.Path), ".profession-index-*")
	if e != nil {
		return report, e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	h := make([]byte, professionIndexHeader)
	if _, e = f.Write(h); e != nil {
		return report, e
	}
	offset := int64(professionIndexHeader)
	writeRecord := func(v any) (professionIndexRecord, error) {
		var r professionIndexRecord
		if e := ctx.Err(); e != nil {
			return r, e
		}
		raw, e := json.Marshal(v)
		if e != nil {
			return r, e
		}
		if len(raw) > professionRecordLimit {
			return r, errors.New("profession payload too large")
		}
		var buf bytes.Buffer
		z := zlib.NewWriter(&buf)
		if _, e = z.Write(raw); e != nil {
			return r, e
		}
		if e = z.Close(); e != nil {
			return r, e
		}
		if buf.Len() > professionRecordLimit {
			return r, errors.New("compressed profession payload too large")
		}
		raw = buf.Bytes()
		digest := sha256.Sum256(raw)
		if _, e = f.Write(raw); e != nil {
			return r, e
		}
		r = professionIndexRecord{Offset: offset, Size: len(raw), SHA256: hex.EncodeToString(digest[:])}
		offset += int64(len(raw))
		return r, nil
	}
	manifest := professionIndexManifest{}
	for _, entry := range entries {
		if entry.ID < 0 || entry.ID > 255 {
			return report, profession.ErrProfileMissing
		}
		text, e := source.ReadText(entry.Path)
		if e != nil {
			return report, e
		}
		job := byte(entry.ID)
		profiles, e := profession.ProfilesFromText(job, text)
		if e != nil {
			return report, e
		}
		name, ok := profiles.DisplayName(job, 0)
		if !ok {
			return report, profession.ErrProfileMissing
		}
		r, e := writeRecord(professionJobPayload{Job: job, Profile: text, Skills: table.SkillsForJob(job)})
		if e != nil {
			return report, e
		}
		manifest.Jobs = append(manifest.Jobs, professionIndexJob{Job: job, Name: name, Choices: profiles.Choices(job), Record: r})
	}
	// The existing quest importer can reuse its independently verified generation.
	qr, e := BuildQuestIndex(ctx, source, summary)
	if e != nil {
		return report, e
	}
	qi, e := readQuestIndex(ctx, qr.Path, hash)
	if e != nil {
		return report, e
	}
	definitions := []quest.Definition{}
	for _, row := range qi.Rows {
		d, e := qi.definition(ctx, row.ID)
		if e != nil {
			return report, e
		}
		if d.JobChangeQuest >= 1 && d.JobChangeQuest <= 3 {
			definitions = append(definitions, d)
		}
	}
	manifest.Quests, e = writeRecord(definitions)
	if e != nil {
		return report, e
	}
	raw, e := json.Marshal(manifest)
	if e != nil {
		return report, e
	}
	if len(raw) > 1<<20 {
		return report, errors.New("profession manifest too large")
	}
	if _, e = f.Write(raw); e != nil {
		return report, e
	}
	copy(h[:8], "GMPROF01")
	digest, _ := hex.DecodeString(hash)
	copy(h[8:40], digest)
	binary.LittleEndian.PutUint64(h[40:48], professionIndexVersion)
	binary.LittleEndian.PutUint64(h[48:56], uint64(offset))
	binary.LittleEndian.PutUint64(h[56:64], uint64(len(raw)))
	sum := sha256.Sum256(raw)
	copy(h[64:96], sum[:])
	if _, e = f.WriteAt(h, 0); e != nil {
		return report, e
	}
	if e = f.Sync(); e != nil {
		return report, e
	}
	if e = f.Close(); e != nil {
		return report, e
	}
	ix, e := readProfessionIndex(ctx, tmp, hash)
	if e != nil {
		return report, e
	}
	verified, e := validateProfessionIndex(ctx, ix)
	if e != nil {
		return report, e
	}
	if e = ctx.Err(); e != nil {
		return report, e
	}
	if e = os.Rename(tmp, report.Path); e != nil {
		return report, e
	}
	verified.Path = report.Path
	return verified, nil
}
