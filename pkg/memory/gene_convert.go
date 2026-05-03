package memory

import (
	"strings"
)

// FromEvolutionGene converts an evolution.Gene (from the GEP pipeline) to a memory.Gene.
// This is used when Raft-broadcast genes are persisted into the semantic store.
// The caller must provide the Gene value with at minimum: Role, Content, Tags.
func FromEvolutionGene(role, controlSignal string, weight float64, skills, tags []string) *Gene {
	// Combine control signal and skills into Content
	content := controlSignal
	if content == "" {
		// Fallback: use skills as content
		content = strings.Join(skills, "; ")
	}

	allTags := make([]string, 0, len(tags)+len(skills))
	allTags = append(allTags, tags...)
	for _, s := range skills {
		allTags = append(allTags, "skill:"+s)
	}

	if weight <= 0 {
		weight = 1.0
	}

	return &Gene{
		Role:    role,
		Content: content,
		Weight:  weight,
		Tags:    allTags,
	}
}
