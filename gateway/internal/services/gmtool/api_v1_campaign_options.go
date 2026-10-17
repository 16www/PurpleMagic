package gmtool

import (
	"strconv"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func (s *Server) v1ProjectCampaignPlan(d testbenefits.MailDraft) (testbenefits.Job, string, error) {
	j, name, err := s.v1ResolveReward(v1RewardInput{Title: d.Title, Body: d.Body, Gold: d.Gold, ItemID: d.ItemID, Count: d.Count}, testbenefits.ScopeCharacter)
	if err != nil {
		return j, name, err
	}
	if len(j.Attachments) != 1 {
		return j, name, &v1MailInputError{422, "invalid_reward", "福利规则需要一种物品"}
	}
	if d.PVFChecksum == "" || d.PVFChecksum != s.v1PVFChecksum() {
		return j, name, &v1MailInputError{409, "pvf_conflict", "PVF 已变更，请重新读取发放参数"}
	}
	entry, _ := s.items.lookup(uint32(j.Attachments[0].ItemID))
	o := gmMailGrantOptions{ExpireDays: d.Options.ExpireDays, Reinforce: d.Options.Reinforce, AmplifyType: d.Options.AmplifyType, EquipmentQuality: d.Options.EquipmentQuality, ForgingLevel: d.Options.ForgingLevel}
	if rejection := s.validateGMMailGrantOptions(entry, int(d.Count), o); rejection != nil {
		return j, name, &v1MailInputError{422, "invalid_options", rejection.message}
	}
	quality, _ := normalizeGMMailEquipmentQuality(d.Options.EquipmentQuality)
	options := d.Options
	options.EquipmentQuality = quality
	d.Options.ExpireDays = 0
	if quality == gmQualityRandom {
		d.Options.EquipmentQuality = gmQualityDefault
	}
	j, err = s.v1ProjectMailDraft(d, time.Time{})
	if err != nil {
		return j, name, err
	}
	j.RewardPlan = &testbenefits.RewardPlan{Version: 1, Options: options}
	j.PVFChecksum = d.PVFChecksum
	j.Definition, err = s.v1CampaignDefinition(entry)
	if err != nil {
		return j, name, err
	}
	if d.PVFChecksum != s.v1PVFChecksum() {
		return j, name, testbenefits.ErrPVFReview
	}
	return j, name, nil
}

func (s *Server) v1ProjectCampaignDelivery(j testbenefits.Job, at time.Time) ([]repository.MailAttachment, error) {
	if j.RewardPlan == nil || j.Validate() != nil {
		return nil, testbenefits.ErrRewardProjection
	}
	a := j.Attachments[0]
	d := testbenefits.MailDraft{Title: j.Title, Body: j.Body, Gold: j.Gold, ItemID: strconv.FormatInt(a.ItemID, 10), Count: a.Count, Options: j.RewardPlan.Options, PVFChecksum: j.PVFChecksum}
	resolved, err := s.v1ProjectMailDraft(d, at)
	return resolved.Attachments, err
}
