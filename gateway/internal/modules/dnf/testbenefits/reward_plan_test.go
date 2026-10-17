package testbenefits_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

func TestItemRulePlanRollbackAndReceipt(t *testing.T) {
	g, j, r := fixture(t)
	ctx := context.Background()
	j.CampaignID = strings.Repeat("b", 32)
	j.PVFChecksum = strings.Repeat("a", 64)
	j.RewardPlan = &testbenefits.RewardPlan{Version: 1, Options: testbenefits.MailOptions{ExpireDays: 7}}
	j.Attachments = []repository.MailAttachment{{ItemID: 1, Count: 1}}
	project := func(j testbenefits.Job, at time.Time) ([]repository.MailAttachment, error) {
		return []repository.MailAttachment{{ItemID: 1, Count: 1, ExpireAt: at.Add(7 * 24 * time.Hour)}}, nil
	}
	if _, e := testbenefits.DeliverWithReceipt(ctx, g, j, r); !errors.Is(e, testbenefits.ErrRewardProjection) {
		t.Fatal(e)
	}
	broken := g
	broken.MailboxAssets = failingReceipt{g.MailboxAssets, g.MailboxAssets.(repository.AccountMailboxCharacterUnitOfWork)}
	if _, e := testbenefits.DeliverProjectedWithReceipt(ctx, broken, j, r, project); e == nil {
		t.Fatal("receipt failure accepted")
	}
	box, _, e := g.Mailbox.Load(ctx, r.CharacterID)
	require(t, e)
	if len(box.Mails) != 0 {
		t.Fatal("failed receipt left mail")
	}
	first, e := testbenefits.DeliverProjectedWithReceipt(ctx, g, j, r, project)
	require(t, e)
	second, e := testbenefits.DeliverWithReceipt(ctx, g, j, r)
	require(t, e)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("replay drift")
	}
	j.RewardPlan = &testbenefits.RewardPlan{Version: 1, Options: testbenefits.MailOptions{ExpireDays: 8}}
	if _, e = testbenefits.DeliverProjectedWithReceipt(ctx, g, j, r, project); e == nil {
		t.Fatal("changed plan reused receipt")
	}
}
