package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Citation records when a knowledge entry materially contributed to an answer.
type Citation struct {
	MemoryKey string    `json:"memory_key"`
	Context   string    `json:"context"`    // brief description of how it was used
	Timestamp time.Time `json:"timestamp"`
	Useful    bool      `json:"useful"`     // was the entry actually helpful?
}

// CitationLog persists citation data for knowledge feedback.
type CitationLog struct {
	Citations []Citation `json:"citations"`
	Updated   time.Time  `json:"updated"`
}

func citationPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Metrics", "citations.json")
}

// LoadCitations reads the citation log from disk.
func LoadCitations(vaultRoot string) (*CitationLog, error) {
	path := citationPath(vaultRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CitationLog{}, nil
		}
		return nil, err
	}

	var log CitationLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, err
	}
	return &log, nil
}

// SaveCitations writes the citation log to disk.
func SaveCitations(vaultRoot string, log *CitationLog) error {
	path := citationPath(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	log.Updated = time.Now()
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// RecordCitation adds a citation entry.
func RecordCitation(log *CitationLog, memoryKey, context string, useful bool) {
	log.Citations = append(log.Citations, Citation{
		MemoryKey: memoryKey,
		Context:   truncate(context, 200),
		Timestamp: time.Now(),
		Useful:    useful,
	})
}

// CitationStats returns usage statistics for a specific memory.
type CitationStats struct {
	TotalCitations int
	UsefulCount    int
	LastCited      time.Time
	Contexts       []string
}

// GetCitationStats computes citation statistics for a memory key.
func GetCitationStats(log *CitationLog, memoryKey string) CitationStats {
	var stats CitationStats
	for _, c := range log.Citations {
		if c.MemoryKey == memoryKey {
			stats.TotalCitations++
			if c.Useful {
				stats.UsefulCount++
			}
			if c.Timestamp.After(stats.LastCited) {
				stats.LastCited = c.Timestamp
			}
			if len(stats.Contexts) < 5 {
				stats.Contexts = append(stats.Contexts, c.Context)
			}
		}
	}
	return stats
}

// GetAllCitationStats returns stats for all cited memories, sorted by citation count.
func GetAllCitationStats(log *CitationLog) []struct {
	Key   string
	Stats CitationStats
} {
	statsMap := make(map[string]*CitationStats)
	for _, c := range log.Citations {
		s, ok := statsMap[c.MemoryKey]
		if !ok {
			s = &CitationStats{}
			statsMap[c.MemoryKey] = s
		}
		s.TotalCitations++
		if c.Useful {
			s.UsefulCount++
		}
		if c.Timestamp.After(s.LastCited) {
			s.LastCited = c.Timestamp
		}
		if len(s.Contexts) < 5 {
			s.Contexts = append(s.Contexts, c.Context)
		}
	}

	var result []struct {
		Key   string
		Stats CitationStats
	}
	for k, s := range statsMap {
		result = append(result, struct {
			Key   string
			Stats CitationStats
		}{k, *s})
	}

	// Sort by total citations descending
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Stats.TotalCitations > result[i].Stats.TotalCitations {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}
