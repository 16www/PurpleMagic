package profession

import (
	"fmt"
	"sort"
	"strings"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/skill"
)

// Choice is an actual character/*.chr section, not DisplayName's fallback.
type Choice struct {
	GrowType     byte   `json:"growType"`
	Name         string `json:"name"`
	MinimumLevel int    `json:"minimumLevel"`
}

// ProfilesFromText restores one job from an offline, checksum-verified record.
func ProfilesFromText(job byte, text string) (*Profiles, error) {
	p, err := parseJobProfile(text)
	if err != nil {
		return nil, err
	}
	return &Profiles{jobs: map[byte]jobProfile{job: p}}, nil
}

func (p *Profiles) Choices(job byte) []Choice {
	profile, ok := p.profile(job)
	if !ok {
		return nil
	}
	result := []Choice{}
	for first := byte(0); first < 16; first++ {
		if !profile.growSupported[first] {
			continue
		}
		name := strings.TrimSpace(profile.displayNames[first])
		if name == "" {
			continue
		}
		minimum := 1
		if first > 0 {
			minimum = 15
		}
		result = append(result, Choice{GrowType: first, Name: name, MinimumLevel: minimum})
		// The current quest and skill contracts support the two declared stages.
		for stage := byte(1); stage <= 2; stage++ {
			if !profile.awakeningSupported[first][stage] {
				break
			}
			name = strings.TrimSpace(profile.awakeningNames[first][stage])
			if name == "" {
				continue
			}
			minimum = 50
			if stage == 2 {
				minimum = 75
			}
			result = append(result, Choice{GrowType: stage<<4 | first, Name: name, MinimumLevel: minimum})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GrowType < result[j].GrowType })
	return result
}

type ManagementPlan struct {
	Transition  Transition
	ResetSkills bool
	Skills      repository.SkillRecord
}

// PlanManagementTransition computes a detached change; its caller owns the
// character, skill, quest and receipt transaction. A job/branch change or
// awakening downgrade rebuilds the free baseline and returns spent SP/TP.
func (p *Profiles) PlanManagementTransition(catalog *skill.Table, oldJob, oldGrow, job, grow byte, level int, record repository.SkillRecord) (ManagementPlan, error) {
	var result ManagementPlan
	if oldJob == job && oldGrow == grow {
		return result, ErrTransitionOutOfStep
	}
	if level < 1 || level > 90 {
		return result, ErrTransitionInvalid
	}
	valid := false
	for _, choice := range p.Choices(job) {
		if choice.GrowType == grow && level >= choice.MinimumLevel {
			valid = true
			break
		}
	}
	if !valid {
		return result, fmt.Errorf("%w: job=%d grow=%d level=%d", ErrTransitionInvalid, job, grow, level)
	}
	points := record.Points
	if points.SyncedLevel != level || points.TotalSP < 0 || points.TotalSP > 65535 || points.TotalTP < 0 || points.TotalTP > 65535 || points.RemainingSP < 0 || points.RemainingSP > points.TotalSP || points.RemainingTP < 0 || points.RemainingTP > points.TotalTP {
		return result, fmt.Errorf("%w: skill points", ErrTransitionInvalid)
	}
	first, stage := Decode(grow)
	oldFirst, oldStage := Decode(oldGrow)
	result.ResetSkills = oldJob != job || oldFirst != first || stage < oldStage
	kind := KindAwakening
	if result.ResetSkills {
		kind = KindClassChange
	}
	result.Transition = Transition{Kind: kind, PreviousGrowType: oldGrow, NewGrowType: grow, FirstGrowType: first, AwakeningStage: stage}
	updated, err := p.ApplySkillTransition(catalog, job, level, result.Transition, record, points)
	if err != nil {
		return ManagementPlan{}, err
	}
	result.Skills = updated
	return result, nil
}
