package gmtool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"

	"dnf90.local/gateway/internal/modules/dnf/expertjob"
	"dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/quest"
)

type expertJobIndex struct {
	Version     int                    `json:"version"`
	PVFChecksum string                 `json:"pvfChecksum"`
	Jobs        []expertjob.Definition `json:"jobs"`
	QuestIDs    []int64                `json:"questIds"`
}

func (x *expertJobIndex) validate(hash string) error {
	if x.Version != 1 || x.PVFChecksum != hash || len(x.Jobs) != 4 || len(x.QuestIDs) == 0 || len(x.QuestIDs) > 256 {
		return expertjob.ErrDefinition
	}
	all := map[int64]bool{}
	last := int64(0)
	for _, id := range x.QuestIDs {
		if id <= last {
			return expertjob.ErrDefinition
		}
		all[id] = true
		last = id
	}
	for i, d := range x.Jobs {
		if d.Type != i+1 || d.Validate() != nil {
			return expertjob.ErrDefinition
		}
		for _, id := range d.QuestIDs {
			if !all[id] {
				return expertjob.ErrDefinition
			}
		}
	}
	return nil
}
func expertJobIndexPath(summary, hash string) string {
	b, e := hex.DecodeString(hash)
	if e != nil || len(b) != 32 || summary == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(summary), "expert-jobs-v1-"+hash+".json")
}
func readExpertJobIndex(ctx context.Context, path, hash string) (*expertJobIndex, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(raw) > 1<<20 {
		return nil, expertjob.ErrDefinition
	}
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
		SHA256  string          `json:"sha256"`
	}
	if e = json.Unmarshal(raw, &envelope); e != nil {
		return nil, e
	}
	sum := sha256.Sum256(envelope.Payload)
	if hex.EncodeToString(sum[:]) != envelope.SHA256 {
		return nil, errors.New("expert job index checksum mismatch")
	}
	var x expertJobIndex
	if e = json.Unmarshal(envelope.Payload, &x); e != nil {
		return nil, e
	}
	if e = x.validate(hash); e != nil {
		return nil, e
	}
	return &x, ctx.Err()
}

type ExpertJobIndexReport struct {
	Path        string `json:"path"`
	PVFChecksum string `json:"pvfChecksum"`
	Jobs        int    `json:"jobs"`
	Quests      int    `json:"quests"`
	Bytes       int    `json:"bytes"`
	Reused      bool   `json:"reused"`
}

func BuildExpertJobIndex(ctx context.Context, source pvf.Source, summary string) (ExpertJobIndexReport, error) {
	hash := itemIndexSourceChecksum(source)
	r := ExpertJobIndexReport{Path: expertJobIndexPath(summary, hash), PVFChecksum: hash}
	if r.Path == "" {
		return r, expertjob.ErrDefinition
	}
	if x, e := readExpertJobIndex(ctx, r.Path, hash); e == nil {
		info, e := os.Stat(r.Path)
		if e != nil {
			return r, e
		}
		r.Jobs = len(x.Jobs)
		r.Quests = len(x.QuestIDs)
		r.Bytes = int(info.Size())
		r.Reused = true
		return r, nil
	}
	if e := ctx.Err(); e != nil {
		return r, e
	}
	qr, e := BuildQuestIndex(ctx, source, summary)
	if e != nil {
		return r, e
	}
	qi, e := readQuestIndex(ctx, qr.Path, hash)
	if e != nil {
		return r, e
	}
	definitions := []quest.Definition{}
	x := expertJobIndex{Version: 1, PVFChecksum: hash}
	for _, row := range qi.Rows {
		d, e := qi.definition(ctx, row.ID)
		if e != nil {
			return r, e
		}
		if d.JobChangeQuest == 20 {
			definitions = append(definitions, d)
			x.QuestIDs = append(x.QuestIDs, d.ID)
		}
	}
	sort.Slice(x.QuestIDs, func(i, j int) bool { return x.QuestIDs[i] < x.QuestIDs[j] })
	for i, path := range expertjob.Paths {
		if e = ctx.Err(); e != nil {
			return r, e
		}
		text, e := source.ReadText(path)
		if e != nil {
			return r, e
		}
		d, e := expertjob.ParseManagement(i+1, text, definitions)
		if e != nil {
			return r, e
		}
		x.Jobs = append(x.Jobs, d)
	}
	if e = x.validate(hash); e != nil {
		return r, e
	}
	raw, e := json.Marshal(x)
	if e != nil {
		return r, e
	}
	sum := sha256.Sum256(raw)
	raw, e = json.Marshal(struct {
		Payload json.RawMessage `json:"payload"`
		SHA256  string          `json:"sha256"`
	}{raw, hex.EncodeToString(sum[:])})
	if e != nil {
		return r, e
	}
	if e = os.MkdirAll(filepath.Dir(r.Path), 0700); e != nil {
		return r, e
	}
	f, e := os.CreateTemp(filepath.Dir(r.Path), ".expert-job-index-*")
	if e != nil {
		return r, e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if _, e = f.Write(raw); e != nil {
		return r, e
	}
	if e = f.Sync(); e != nil {
		return r, e
	}
	if e = f.Close(); e != nil {
		return r, e
	}
	if _, e = readExpertJobIndex(ctx, tmp, hash); e != nil {
		return r, e
	}
	if e = os.Rename(tmp, r.Path); e != nil {
		return r, e
	}
	r.Jobs = len(x.Jobs)
	r.Quests = len(x.QuestIDs)
	r.Bytes = len(raw)
	return r, nil
}
func (s *Server) expertJobResources(ctx context.Context) (*expertJobIndex, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if s.pvf == nil || s.items == nil || s.deps.PVF == nil {
		return nil, expertjob.ErrDefinition
	}
	source, e := s.source()
	if e != nil {
		return nil, e
	}
	hash := itemIndexSourceChecksum(source)
	s.pvf.mu.Lock()
	defer s.pvf.mu.Unlock()
	if s.pvf.managementExpertJob == nil || s.pvf.managementExpertJob.PVFChecksum != hash {
		s.pvf.managementExpertJob = nil
		x, e := readExpertJobIndex(ctx, expertJobIndexPath(s.items.cachePath, hash), hash)
		if e != nil {
			return nil, e
		}
		s.pvf.managementExpertJob = x
	}
	return s.pvf.managementExpertJob, nil
}
