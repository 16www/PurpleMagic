package gmtool

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/profession"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
)

func professionIndexFixture(name string) *itemCacheTestSource {
	d := readonlyTestSource{
		profession.DefaultCharacterList: "0 `zero.chr`\n2 `two.chr`\n",
		"skill/skilllist.lst":           "0 `zero.lst`\n2 `two.lst`\n",
		"skill/zero.lst":                "1 `zero/a.skl`\n2 `zero/b.skl`\n3 `zero/c.skl`\n",
		"skill/two.lst":                 "1 `two/a.skl`\n2 `two/b.skl`\n3 `two/c.skl`\n",
		"n_quest/quest.lst":             "1 `story.qst`\n2 `job.qst`\n3 `awake.qst`\n4 `expert.qst`\n",
		"n_quest/story.qst":             "[name]\n`故事`\n[level]\n1 90\n",
		"n_quest/job.qst":               "[name]\n`转职`\n[level]\n15 90\n[job]\n`[gunner]`\n[job change quest]\n1\n[reward type]\n`[grow type]`\n[reward int data]\n1\n",
		"n_quest/awake.qst":             "[name]\n`觉醒`\n[level]\n50 90\n[job]\n`[gunner]`\n[job change quest]\n2\n",
		"n_quest/expert.qst":            "[name]\n`副职业`\n[level]\n20 90\n[job change quest]\n4\n",
	}
	for _, job := range []string{"zero", "two"} {
		d["character/"+job+".chr"] = "[growtype name]\n`" + name + job + "` `分支`\n[initial value]\n[skill]\n1 1\n[/skill]\n[growtype 1]\n[growtype 2]\n[skill]\n2 1\n197 1\n[/skill]\n[awakening name]\n`一觉` `仅有名字`\n[awakening 1]\n[awakening skill]\n3 1\n[/awakening skill]\n"
		for _, file := range []string{"a", "b", "c"} {
			d["skill/"+job+"/"+file+".skl"] = "[name]\n`" + job + file + "`\n[skill type]\n`[active]`\n"
		}
	}
	return newItemCacheTestSource(d)
}
func TestProfessionIndexGenerationIntegrityAndBoundedResources(t *testing.T) {
	ctx := context.Background()
	summary := filepath.Join(t.TempDir(), "items.json")
	source := professionIndexFixture("旧")
	report, e := BuildProfessionIndex(ctx, source, summary)
	if e != nil {
		t.Fatal(e)
	}
	if report.Jobs != 2 || report.Skills != 6 || report.Quests != 2 || report.Reused {
		t.Fatal(report)
	}
	ix, e := readProfessionIndex(ctx, report.Path, source.checksum)
	if e != nil {
		t.Fatal(e)
	}
	job, e := ix.job(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	if job.Skills.Snapshot().Jobs != 1 || len(job.Profiles.Choices(2)) != 3 {
		t.Fatal("unexpected runtime scope")
	}
	if _, ok := job.Skills.Find(0, 1); ok {
		t.Fatal("other job retained")
	}
	q, e := ix.quests(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if got := q.ProfessionProgressionQuestIDs(2, 17); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Fatal(got)
	}
	if _, ok := q.Find(1); ok {
		t.Fatal("story retained")
	}
	if _, ok := q.Find(4); ok {
		t.Fatal("expert retained")
	}
	reads := source.reads
	warm, e := BuildProfessionIndex(ctx, source, summary)
	if e != nil || !warm.Reused || source.reads != reads {
		t.Fatal("warm import read PVF", warm, e)
	}
	s := &Server{pvf: newPVFCache(), deps: Deps{PVF: func() (dnfpvf.Source, error) { return source, nil }}}
	s.items = newItemIndex(summary, s, nil)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := byte(0)
			if i%2 == 1 {
				id = 2
			}
			_, r, _, e := s.professionManagementResources(ctx, id)
			if e != nil {
				t.Error(e)
				return
			}
			if r.Skills.Snapshot().Jobs != 1 {
				t.Error("more than selected job")
			}
		}(i)
	}
	wg.Wait()
	if source.reads != reads {
		t.Fatal("runtime read PVF texts")
	}
	if _, _, _, e := s.professionManagementResources(ctx, 1); !errors.Is(e, profession.ErrProfileMissing) {
		t.Fatal(e)
	}
	old := source
	source = professionIndexFixture("新")
	if _, _, _, e := s.professionManagementResources(ctx, 2); e == nil || s.pvf.managementProfession != nil {
		t.Fatal("stale generation used")
	}
	next, e := BuildProfessionIndex(ctx, source, summary)
	if e != nil || next.Path == report.Path {
		t.Fatal(next, e)
	}
	_, newJob, _, e := s.professionManagementResources(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	if name, _ := newJob.Profiles.DisplayName(2, 0); name != "新two" {
		t.Fatal(name)
	}
	if name, _ := job.Profiles.DisplayName(2, 0); name != "旧two" {
		t.Fatal("old immutable result mutated")
	}
	if _, e = readProfessionIndex(ctx, report.Path, source.checksum); e == nil {
		t.Fatal("wrong hash accepted")
	}
	raw, e := os.ReadFile(report.Path)
	if e != nil {
		t.Fatal(e)
	}
	for _, offset := range []int{0, 40, 64, int(ix.Manifest.Jobs[0].Record.Offset)} {
		bad := append([]byte(nil), raw...)
		bad[offset] ^= 1
		if e = os.WriteFile(report.Path, bad, 0600); e != nil {
			t.Fatal(e)
		}
		check, e := readProfessionIndex(ctx, report.Path, old.checksum)
		if e == nil {
			_, e = validateProfessionIndex(ctx, check)
		}
		if e == nil {
			t.Fatal("corruption accepted", offset)
		}
	}
	if _, e = BuildProfessionIndex(ctx, old, summary); e != nil {
		t.Fatal("rebuild corrupted generation", e)
	}
	if _, e = ix.job(ctx, 0); e != nil {
		t.Fatal("identical rebuild changed content", e)
	}
	bad := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint64(bad[56:64], ^uint64(0))
	if e = os.WriteFile(report.Path, bad, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = readProfessionIndex(ctx, report.Path, old.checksum); e == nil {
		t.Fatal("unbounded manifest")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = BuildProfessionIndex(canceled, old, summary); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, _, _, e = s.professionManagementResources(canceled, 0); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	// A failed new generation cannot replace the working new index.
	kept, e := os.ReadFile(next.Path)
	if e != nil {
		t.Fatal(e)
	}
	broken := professionIndexFixture("损坏")
	delete(broken.readonlyTestSource, "skill/two/a.skl")
	if _, e = BuildProfessionIndex(ctx, broken, summary); e == nil {
		t.Fatal("missing skill accepted")
	}
	after, e := os.ReadFile(next.Path)
	if e != nil || !reflect.DeepEqual(kept, after) {
		t.Fatal("other generation changed", e)
	}
	files, e := filepath.Glob(filepath.Join(filepath.Dir(summary), ".profession-index-*"))
	if e != nil || len(files) > 0 {
		t.Fatal("temporary files", files, e)
	}
	if professionIndexPath(summary, strings.Repeat("z", 64)) != "" {
		t.Fatal("invalid digest path")
	}
}
