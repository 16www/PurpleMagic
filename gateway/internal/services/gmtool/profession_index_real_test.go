package gmtool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/profession"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/quest"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/skill"
)

func TestRealProfessionIndexCompiled(t *testing.T) {
	out := os.Getenv("GM_PROFESSION_INDEX_AUDIT")
	if out == "" {
		t.Skip("set GM_PROFESSION_INDEX_AUDIT and real PVF disk source variables for offline audit")
	}
	if e := os.MkdirAll(out, 0700); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	source := loadRealPVFSource(t)
	summary := filepath.Join(out, "items.json")
	start := time.Now()
	report, e := BuildProfessionIndex(ctx, source, summary)
	if e != nil {
		t.Fatal(e)
	}
	elapsed := time.Since(start)
	ix, e := readProfessionIndex(ctx, report.Path, report.PVFChecksum)
	if e != nil {
		t.Fatal(e)
	}
	comparisons := compareRealProfessionIndex(t, source, ix)
	warm, e := BuildProfessionIndex(ctx, source, summary)
	if e != nil || !warm.Reused {
		t.Fatal(warm, e)
	}
	s := &Server{pvf: newPVFCache(), deps: Deps{PVF: func() (dnfpvf.Source, error) { return source, nil }}}
	s.items = newItemIndex(summary, s, nil)
	for _, j := range ix.Manifest.Jobs {
		_, r, _, e := s.professionManagementResources(ctx, j.Job)
		if e != nil {
			t.Fatal(e)
		}
		if r.Skills.Snapshot().Jobs != 1 {
			t.Fatal("runtime retained multiple jobs")
		}
	}
	runtime.GC()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	result := map[string]any{"build": report, "buildMilliseconds": elapsed.Milliseconds(), "verifiedTransitions": comparisons, "warmReused": true, "retainedHeapBytes": mem.HeapAlloc, "cachedSkillJobs": s.pvf.managementProfession.resources.Skills.Snapshot().Jobs, "cachedSkills": s.pvf.managementProfession.resources.Skills.Snapshot().Skills, "databaseConnected": false, "httpStarted": false}
	raw, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(out, "profession-receipt.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log(string(raw))
	runtime.KeepAlive(s)
}

func compareRealProfessionIndex(t *testing.T, source dnfpvf.Source, ix *professionDiskIndex) int {
	t.Helper()
	ctx := context.Background()
	index, e := dnfpvf.Build(ctx, source, dnfpvf.BuildOptions{Lists: []string{skill.DefaultList, quest.DefaultList}})
	if e != nil {
		t.Fatal(e)
	}
	full, e := skill.Load(ctx, index, skill.Options{})
	if e != nil {
		t.Fatal(e)
	}
	profiles, e := profession.LoadProfiles(ctx, source, full)
	if e != nil {
		t.Fatal(e)
	}
	allQuests, e := quest.Load(ctx, index)
	if e != nil {
		t.Fatal(e)
	}
	smallQuests, e := ix.quests(ctx)
	if e != nil {
		t.Fatal(e)
	}
	comparisons := 0
	for _, j := range ix.Manifest.Jobs {
		job, e := ix.job(ctx, j.Job)
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(full.SkillsForJob(j.Job), job.Skills.Skills()) {
			t.Fatal("skill definition drift", j.Job)
		}
		if !reflect.DeepEqual(profiles.Choices(j.Job), j.Choices) {
			t.Fatal("choice drift", j.Job)
		}
		for _, choice := range j.Choices {
			if !reflect.DeepEqual(allQuests.ProfessionProgressionQuestIDs(int(j.Job), int(choice.GrowType)), smallQuests.ProfessionProgressionQuestIDs(int(j.Job), int(choice.GrowType))) {
				t.Fatal("quest selection drift", j.Job, choice)
			}
			previous := byte(0)
			if choice.GrowType == 0 {
				previous = 33
			}
			record := repository.SkillRecord{CharacterID: "10", Points: repository.SkillPointState{TotalSP: 10000, RemainingSP: 5000, TotalTP: 100, RemainingTP: 50, SyncedLevel: 90}}
			fromDisk, e := job.Profiles.PlanManagementTransition(job.Skills, j.Job, previous, j.Job, choice.GrowType, 90, record)
			if e != nil {
				t.Fatal("disk plan", j.Job, choice, e)
			}
			original, e := profiles.ApplySkillTransition(full, j.Job, 90, fromDisk.Transition, record, record.Points)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(fromDisk.Skills, original) {
				t.Fatal("transition drift", j.Job, choice)
			}
			comparisons++
		}
	}
	return comparisons
}
