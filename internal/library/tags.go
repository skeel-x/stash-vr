package library

import (
	"context"
	"fmt"
	"slices"
	"stash-vr/internal/config"
	"stash-vr/internal/prefix"
	"stash-vr/internal/stash/gql"
	"stash-vr/internal/util"
	"strings"
)

type Tag struct {
	Id        string
	Name      string
	SortName  string
	Aliases   []string
	ParentIds []string
}

// LoadTags fetches all tags from Stash and replaces the tag cache. It is
// called on every index build so tag hierarchy changes in Stash show up
// without a restart.
func (libraryService *Service) LoadTags(ctx context.Context) error {
	resp, err := gql.FindAllTags(ctx, libraryService.StashClient)
	if err != nil {
		return err
	}
	tagCache := make(map[string]*Tag, len(resp.FindTags.Tags))
	for _, st := range resp.FindTags.Tags {
		t := Tag{
			Id:       st.Id,
			Name:     st.Name,
			SortName: util.FirstNonEmpty(&st.Sort_name, &st.Name),
			Aliases:  st.Aliases,
		}

		for _, p := range st.Parents {
			t.ParentIds = append(t.ParentIds, p.Id)
		}
		tagCache[st.Id] = &t
	}

	libraryService.muTagCache.Lock()
	libraryService.tagCache = tagCache
	libraryService.muTagCache.Unlock()
	return nil
}

func (libraryService *Service) GetBrowseTags(ctx context.Context) ([]Tag, error) {
	if err := libraryService.LoadTags(ctx); err != nil {
		return nil, fmt.Errorf("LoadTags: %w", err)
	}

	libraryService.muTagCache.RLock()
	defer libraryService.muTagCache.RUnlock()

	tags := make([]Tag, 0, len(libraryService.tagCache))
	for _, tag := range libraryService.tagCache {
		if tag == nil {
			continue
		}
		if tag.SortName == config.Application().ExcludeSortName || strings.HasPrefix(tag.SortName, prefix.SvrAncestor) {
			continue
		}
		tags = append(tags, *tag)
	}

	slices.SortFunc(tags, func(a Tag, b Tag) int {
		if cmp := strings.Compare(strings.ToLower(a.SortName), strings.ToLower(b.SortName)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Id, b.Id)
	})

	return tags, nil
}

func (libraryService *Service) GetTagSceneIDs(ctx context.Context, tagID string) ([]string, error) {
	allPages := -1
	resp, err := gql.FindSceneIdsByFilter(ctx, libraryService.StashClient,
		&gql.SceneFilterType{Tags: &gql.HierarchicalMultiCriterionInput{Modifier: gql.CriterionModifierIncludes, Value: []string{tagID}}},
		&gql.FindFilterType{Per_page: &allPages},
	)
	if err != nil {
		return nil, fmt.Errorf("FindSceneIdsByFilter(tag): %w", err)
	}

	ids := make([]string, len(resp.FindScenes.Scenes))
	for i, scene := range resp.FindScenes.Scenes {
		ids[i] = scene.Id
	}
	return ids, nil
}

func (libraryService *Service) GetSceneBrowseTags(vd *VideoData) []Tag {
	if vd == nil || vd.SceneParts == nil {
		return nil
	}

	realTags := map[string]Tag{}
	for _, sceneTag := range vd.SceneParts.Tags {
		if sceneTag == nil {
			continue
		}
		if sceneTag.Sort_name == config.Application().ExcludeSortName || strings.HasPrefix(sceneTag.Sort_name, prefix.SvrAncestor) {
			continue
		}
		realTags[sceneTag.Id] = Tag{Id: sceneTag.Id, Name: sceneTag.Name, SortName: sceneTag.Sort_name, Aliases: sceneTag.Aliases}
	}

	tags := make([]Tag, 0, len(realTags))
	for _, tag := range realTags {
		tags = append(tags, tag)
	}
	slices.SortFunc(tags, func(a Tag, b Tag) int {
		if cmp := strings.Compare(strings.ToLower(a.SortName), strings.ToLower(b.SortName)); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Id, b.Id)
	})
	return tags
}

func (libraryService *Service) ancestors(tagId string) []Tag {
	libraryService.muTagCache.RLock()
	defer libraryService.muTagCache.RUnlock()

	visited := map[string]struct{}{tagId: {}}
	queue := []string{tagId}
	out := []Tag{}

	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		t := libraryService.tagCache[id]
		if t == nil {
			continue
		}
		for _, pid := range t.ParentIds {
			if _, seen := visited[pid]; seen {
				continue
			}
			visited[pid] = struct{}{}
			p := libraryService.tagCache[pid]
			if p == nil {
				continue
			}
			queue = append(queue, pid)

			out = append(out, *p)
		}
	}
	return out
}

func (libraryService *Service) decorateTags(vd *VideoData) {
	if vd == nil || vd.SceneParts == nil {
		return
	}

	vd.SceneParts.Tags = slices.DeleteFunc(vd.SceneParts.Tags, func(tag *gql.TagPartsArrayTagsTag) bool {
		if tag == nil {
			return true
		}
		return tag.Sort_name == config.Application().ExcludeSortName
	})

	allAncestors := map[string]Tag{}
	for _, t := range vd.SceneParts.Tags {
		if t == nil || t.Id == "" {
			continue
		}
		ancestors := libraryService.ancestors(t.Id)
		for _, a := range ancestors {
			if a.SortName == config.Application().ExcludeSortName {
				continue
			}
			allAncestors[a.Id] = a
		}
	}

	for _, a := range allAncestors {
		vd.SceneParts.Tags = append(vd.SceneParts.Tags, &gql.TagPartsArrayTagsTag{TagParts: gql.TagParts{
			Id:        a.Id,
			Name:      a.Name,
			Sort_name: prefix.SvrAncestor + a.SortName},
		})
	}
}
