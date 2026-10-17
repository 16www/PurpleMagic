package quest

import (
	"errors"
	"sort"
)

// CatalogFromDefinitions restores already parsed, checksum-verified definitions.
// Callers supply the complete linked family needed by an operation.
func CatalogFromDefinitions(definitions []Definition) (*Catalog, error) {
	if len(definitions) == 0 {
		return nil, ErrCatalogEmpty
	}
	c := &Catalog{ordered: append([]Definition(nil), definitions...), byID: make(map[int64]Definition, len(definitions))}
	for _, d := range definitions {
		if d.ID <= 0 || d.Path == "" {
			return nil, errors.New("invalid quest definition")
		}
		if _, ok := c.byID[d.ID]; ok {
			return nil, errors.New("duplicate quest definition")
		}
		c.byID[d.ID] = d
	}
	sort.Slice(c.ordered, func(i, j int) bool { return c.ordered[i].ID < c.ordered[j].ID })
	return c, nil
}
