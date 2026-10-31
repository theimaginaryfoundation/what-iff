package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entritual "github.com/theimaginaryfoundation/what-iff/ent/ritual"
	"github.com/theimaginaryfoundation/what-iff/ent/user"
	"github.com/theimaginaryfoundation/what-iff/internal/skills"
)

// BackfillRitualSlugs gives every ritual created before the slug column existed a slug derived
// from its name, unique among its owner's skills (ADR 0x035 §1). The editor and import set slugs
// for everything created since, so this finds work only once per deployment. It returns the number
// of rituals it named.
func BackfillRitualSlugs(ctx context.Context, client *ent.Client, logger *zap.Logger) (int, error) {
	unnamed, err := client.Ritual.Query().
		Where(entritual.Slug("")).
		WithOwner().
		Order(ent.Asc(entritual.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("list rituals without a slug: %w", err)
	}
	if len(unnamed) == 0 {
		return 0, nil
	}

	// Slugs already in use, per owner, so a derived slug never collides with one the editor set.
	usedByOwner := map[uuid.UUID]map[string]struct{}{}
	ownerOf := func(r *ent.Ritual) (uuid.UUID, bool) {
		if r.Edges.Owner == nil {
			return uuid.Nil, false
		}
		return r.Edges.Owner.ID, true
	}
	for _, r := range unnamed {
		ownerID, ok := ownerOf(r)
		if !ok {
			continue
		}
		if _, seen := usedByOwner[ownerID]; seen {
			continue
		}
		taken, err := client.Ritual.Query().
			Where(entritual.HasOwnerWith(user.ID(ownerID)), entritual.SlugNEQ("")).
			Select(entritual.FieldSlug).
			Strings(ctx)
		if err != nil {
			return 0, fmt.Errorf("list slugs for owner %s: %w", ownerID, err)
		}
		set := make(map[string]struct{}, len(taken))
		for _, s := range taken {
			set[s] = struct{}{}
		}
		usedByOwner[ownerID] = set
	}

	named := 0
	for _, r := range unnamed {
		ownerID, ok := ownerOf(r)
		if !ok {
			continue
		}
		used := usedByOwner[ownerID]
		slug, err := skills.UniqueSlug(skills.Slugify(r.Name), func(candidate string) (bool, error) {
			_, taken := used[candidate]
			return taken, nil
		})
		if err != nil {
			return named, fmt.Errorf("derive slug for ritual %s: %w", r.ID, err)
		}
		if err := client.Ritual.UpdateOneID(r.ID).SetSlug(slug).Exec(ctx); err != nil {
			return named, fmt.Errorf("set slug for ritual %s: %w", r.ID, err)
		}
		used[slug] = struct{}{}
		named++
	}
	if logger != nil && named > 0 {
		logger.Info("derived slugs for skills created before the slug column", zap.Int("rituals", named))
	}
	return named, nil
}
