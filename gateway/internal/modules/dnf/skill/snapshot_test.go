package skill

import (
	"context"
	"reflect"
	"testing"
)

func TestSkillSnapshotPreservesJobScopedDefinitions(t *testing.T) {
	full, e := Load(context.Background(), testIndex(t), Options{})
	if e != nil {
		t.Fatal(e)
	}
	for _, job := range []byte{0, 1} {
		defs := full.SkillsForJob(job)
		restored, e := TableFromDefinitions(job, defs)
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(restored.Skills(), defs) || restored.Snapshot().Jobs != 1 {
			t.Fatal("snapshot lost definitions")
		}
		if _, ok := restored.Find(1-job, 46); ok {
			t.Fatal("cross-job skill")
		}
		defs[0].Name = "changed"
		if got, _ := restored.Find(job, 46); got.Name == "changed" {
			t.Fatal("mutable input retained")
		}
		original, _ := full.Find(job, 46)
		found, ok := restored.FindPath(job, original.Path)
		if !ok || !reflect.DeepEqual(found, original) {
			t.Fatal(found)
		}
		for _, invalid := range [][]Skill{nil, {original, original}, {full.SkillsForJob(1 - job)[0]}, {{Job: job}}} {
			if _, e := TableFromDefinitions(job, invalid); e == nil {
				t.Fatal("invalid snapshot accepted")
			}
		}
	}
}
