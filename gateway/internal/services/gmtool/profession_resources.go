package gmtool

import (
	"context"
	"errors"

	"dnf90.local/gateway/internal/modules/dnf/quest"
)

// One selected job is retained per PVF generation. Switching jobs replaces it;
// it never accumulates a full skill catalog or parsed PVF documents.
type professionManagementCache struct {
	index     *professionDiskIndex
	quests    *quest.Catalog
	job       byte
	resources professionJobResources
	loaded    bool
}

func (s *Server) professionManagementResources(ctx context.Context, job byte) (*professionDiskIndex, professionJobResources, *quest.Catalog, error) {
	if s.deps.Catalog != nil {
		return s.deps.Catalog.profession(ctx, job)
	}
	fail := func(e error) (*professionDiskIndex, professionJobResources, *quest.Catalog, error) {
		return nil, professionJobResources{}, nil, e
	}
	if e := ctx.Err(); e != nil {
		return fail(e)
	}
	if s.pvf == nil || s.deps.PVF == nil || s.items == nil {
		return fail(errors.New("profession index unavailable"))
	}
	source, e := s.source()
	if e != nil {
		return fail(e)
	}
	hash := itemIndexSourceChecksum(source)
	s.pvf.mu.Lock()
	defer s.pvf.mu.Unlock()
	c := s.pvf.managementProfession
	if c == nil || c.index.Checksum != hash {
		// Drop the old generation even when the new index has not been built.
		s.pvf.managementProfession = nil
		ix, e := readProfessionIndex(ctx, professionIndexPath(s.items.cachePath, hash), hash)
		if e != nil {
			return fail(e)
		}
		q, e := ix.quests(ctx)
		if e != nil {
			return fail(e)
		}
		c = &professionManagementCache{index: ix, quests: q}
		s.pvf.managementProfession = c
	}
	if !c.loaded || c.job != job {
		resources, e := c.index.job(ctx, job)
		if e != nil {
			return fail(e)
		}
		c.resources = resources
		c.job = job
		c.loaded = true
	}
	return c.index, c.resources, c.quests, ctx.Err()
}
