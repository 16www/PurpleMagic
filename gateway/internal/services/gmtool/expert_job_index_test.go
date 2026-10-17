package gmtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/expertjob"
	"dnf90.local/gateway/internal/modules/dnf/pvf"
	platformpvf "dnf90.local/gateway/internal/platform/pvf"
)

func expertJobFixtureSource() *itemCacheTestSource {
	d := readonlyTestSource{"n_quest/quest.lst": "1 `story.qst`\n2 `e1.qst`\n3 `e2.qst`\n4 `e3.qst`\n5 `e4.qst`\n", "n_quest/story.qst": "[name]\n`主线`\n[level]\n1 90\n"}
	for i, p := range expertjob.Paths {
		d[p] = "[expertness exp]\n20 0 0\n133 0 0\n246 0 0\n[endurance initial value]\n300\n"
		if i != 2 {
			d[p] += "[auto learn recipe]\n1 100\n"
		}
		n := string(rune('1' + i))
		d["n_quest/e"+n+".qst"] = "[name]\n`副职业`\n[level]\n20 90\n[job change quest]\n20\n[reward type]\n`[expert job]`\n[reward int data]\n" + n + "\n"
	}
	return newItemCacheTestSource(d)
}
func TestExpertJobIndexIntegrityAndLevels(t *testing.T) {
	ctx := context.Background()
	source := expertJobFixtureSource()
	summary := filepath.Join(t.TempDir(), "items.json")
	report, e := BuildExpertJobIndex(ctx, source, summary)
	if e != nil {
		t.Fatal(e)
	}
	if report.Reused || report.Jobs != 4 || report.Quests != 4 {
		t.Fatal(report)
	}
	x, e := readExpertJobIndex(ctx, report.Path, source.checksum)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range x.Jobs {
		for exp, want := range map[int64]int{0: 1, 19: 1, 20: 2, 132: 2, 133: 3, 246: 3, 4294967295: 3} {
			if d.Level(exp) != want {
				t.Fatal(exp, d.Level(exp))
			}
		}
		for level, want := range map[int]int64{1: 0, 2: 20, 3: 133} {
			got, e := d.Experience(level)
			if e != nil || got != want {
				t.Fatal(got, e)
			}
		}
		if _, e = d.Experience(4); e == nil {
			t.Fatal("invalid level")
		}
	}
	raw, e := os.ReadFile(report.Path)
	if e != nil {
		t.Fatal(e)
	}
	// Warm runtime and importer use only the on-disk compact definitions.
	source.readonlyTestSource = nil
	r, e := BuildExpertJobIndex(ctx, source, summary)
	if e != nil || !r.Reused {
		t.Fatal(r, e)
	}
	s := &Server{pvf: newPVFCache(), items: newItemIndex(summary, nil, nil)}
	s.deps.PVF = func() (pvf.Source, error) { return source, nil }
	if _, e = s.expertJobResources(ctx); e != nil {
		t.Fatal(e)
	}
	source.checksum = strings.Repeat("a", 64)
	if _, e = s.expertJobResources(ctx); e == nil || s.pvf.managementExpertJob != nil {
		t.Fatal("stale generation")
	}
	if _, e = readExpertJobIndex(ctx, report.Path, source.checksum); e == nil {
		t.Fatal("wrong generation")
	}
	bad := append([]byte(nil), raw...)
	bad[len(bad)-5] ^= 1
	if e = os.WriteFile(report.Path, bad, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = readExpertJobIndex(ctx, report.Path, x.PVFChecksum); e == nil {
		t.Fatal("corruption accepted")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = BuildExpertJobIndex(cancelCtx, source, summary); e == nil {
		t.Fatal("canceled import")
	}
	source = expertJobFixtureSource()
	source.readonlyTestSource[expertjob.Paths[0]] = strings.ReplaceAll(source.readonlyTestSource[expertjob.Paths[0]], "133 0 0", "19 0 0")
	if _, e = BuildExpertJobIndex(ctx, source, summary); e == nil {
		t.Fatal("unordered thresholds")
	}
}
func TestExpertJobRealPVF(t *testing.T) {
	file := os.Getenv("GM_EXPERT_JOB_REAL_PVF")
	if file == "" {
		t.Skip("set GM_EXPERT_JOB_REAL_PVF and GM_EXPERT_JOB_DIRECTORY and GM_EXPERT_JOB_SHA256")
	}
	ctx := context.Background()
	source, e := platformpvf.OpenDisk(file, os.Getenv("GM_EXPERT_JOB_DIRECTORY"), os.Getenv("GM_EXPERT_JOB_SHA256"))
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	summary := os.Getenv("GM_EXPERT_JOB_ITEM_INDEX")
	report, e := BuildExpertJobIndex(ctx, source, summary)
	if e != nil {
		t.Fatal(e)
	}
	x, e := readExpertJobIndex(ctx, report.Path, report.PVFChecksum)
	if e != nil {
		t.Fatal(e)
	}
	for i, d := range x.Jobs {
		raw, e := source.ReadText(expertjob.Paths[i])
		if e != nil {
			t.Fatal(e)
		}
		doc, e := pvf.Parse(expertjob.Paths[i], raw)
		if e != nil {
			t.Fatal(e)
		}
		rows, _ := doc.Section("expertness exp")
		thresholds := []int64{}
		for n := 0; n < len(rows); n += 3 {
			thresholds = append(thresholds, rows[n].Int)
		}
		if !reflect.DeepEqual(thresholds, d.Thresholds) {
			t.Fatal(d)
		}
		for exp := int64(0); exp <= thresholds[len(thresholds)-1]+1; exp++ {
			level := 1
			for _, n := range thresholds {
				if exp < n {
					break
				}
				level++
			}
			level = min(level, len(thresholds))
			if d.Level(exp) != level {
				t.Fatal(exp)
			}
		}
		t.Logf("type=%d levels=%d minimumLevel=%d quests=%v", d.Type, len(thresholds), d.MinimumLevel, d.QuestIDs)
	}
	b, _ := json.Marshal(report)
	t.Log(string(b))
}
