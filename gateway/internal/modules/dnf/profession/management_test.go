package profession

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/skill"
)

func managementProfile(t *testing.T) (*Profiles, *skill.Table) {
	t.Helper()
	p, e := ProfilesFromText(0, "[growtype name]\n`基础` `分支一` `分支二` `仅有名字`\n[initial value]\n[skill]\n1 1\n[/skill]\n[growtype 1]\n[growtype 2]\n[skill]\n2 1\n[/skill]\n[awakening name]\n`一觉` `二觉` `仅有三觉名字`\n[awakening 1]\n[awakening skill]\n3 1\n[/awakening skill]\n[awakening 2]\n[awakening skill]\n4 1\n[/awakening skill]\n[growtype 3]\n[skill]\n5 1\n[/skill]\n")
	if e != nil {
		t.Fatal(e)
	}
	defs := []skill.Skill{}
	for _, id := range []uint16{1, 2, 3, 4, 5, 6} {
		defs = append(defs, skill.Skill{Job: 0, ID: id, Path: "skill/fixture", Name: "技能", Active: id == 1})
	}
	c, e := skill.TableFromDefinitions(0, defs)
	if e != nil {
		t.Fatal(e)
	}
	return p, c
}
func TestManagementProfessionChoicesRequireSections(t *testing.T) {
	p, _ := managementProfile(t)
	want := []Choice{{0, "基础", 1}, {1, "分支一", 15}, {2, "分支二", 15}, {17, "一觉", 50}, {33, "二觉", 75}}
	if got := p.Choices(0); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	// DisplayName falls back to the branch name for missing awakening sections.
	if _, ok := p.DisplayName(0, 0x31); !ok {
		t.Fatal("fixture must exhibit fallback")
	}
	if len(p.Choices(1)) != 0 {
		t.Fatal("job leak")
	}
}
func TestManagementProfessionTransitionPreservesAndResets(t *testing.T) {
	p, c := managementProfile(t)
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	old := repository.SkillRecord{CharacterID: "10", Points: repository.SkillPointState{TotalSP: 5000, RemainingSP: 2000, TotalTP: 50, RemainingTP: 20, SyncedLevel: 90}, Skills: map[int64]repository.SkillState{1: {Level: 2, Enabled: true}, 2: {Level: 3, Enabled: true}, 6: {Level: 7, Enabled: true}}, Layouts: map[int]repository.SkillLayout{0: {0: 1}, 1: {1: 6}}, Cooldowns: map[int64]time.Time{6: now}, UpdatedAt: now}
	copy := repository.CloneSkill(old)
	forward, e := p.PlanManagementTransition(c, 0, 1, 0, 33, 90, old)
	if e != nil {
		t.Fatal(e)
	}
	if forward.ResetSkills || forward.Skills.Points != old.Points || forward.Skills.Skills[6].Level != 7 || forward.Skills.Skills[1].Level != 2 || forward.Skills.Skills[4].Level != 1 || !reflect.DeepEqual(forward.Skills.Layouts[1], old.Layouts[1]) || !reflect.DeepEqual(forward.Skills.Cooldowns, old.Cooldowns) {
		t.Fatal(forward)
	}
	for _, args := range [][4]byte{{0, 33, 0, 17}, {0, 33, 0, 0}, {0, 33, 0, 2}, {1, 1, 0, 1}} {
		plan, e := p.PlanManagementTransition(c, args[0], args[1], args[2], args[3], 90, forward.Skills)
		if e != nil {
			t.Fatal(e)
		}
		if !plan.ResetSkills || plan.Skills.Points.RemainingSP != 5000 || plan.Skills.Points.RemainingTP != 50 || len(plan.Skills.Cooldowns) != 0 || len(plan.Skills.Layouts) != 1 || plan.Skills.Skills[6].Level != 0 || plan.Skills.Skills[4].Level != 0 {
			t.Fatal(args, plan)
		}
	}
	if !reflect.DeepEqual(old, copy) {
		t.Fatal("planning mutated caller")
	}
	for _, v := range []struct {
		grow  byte
		level int
	}{{1, 14}, {17, 49}, {33, 74}, {49, 90}, {3, 90}, {255, 90}, {1, 0}, {1, 91}} {
		record := repository.CloneSkill(old)
		record.Points.SyncedLevel = v.level
		if _, e := p.PlanManagementTransition(c, 0, 0, 0, v.grow, v.level, record); !errors.Is(e, ErrTransitionInvalid) {
			t.Fatal(v, e)
		}
	}
	if _, e := p.PlanManagementTransition(c, 0, 1, 0, 1, 90, old); !errors.Is(e, ErrTransitionOutOfStep) {
		t.Fatal(e)
	}
	bad := repository.CloneSkill(old)
	bad.Points.RemainingSP = 5001
	if _, e := p.PlanManagementTransition(c, 0, 1, 0, 17, 90, bad); !errors.Is(e, ErrTransitionInvalid) {
		t.Fatal(e)
	}
}
