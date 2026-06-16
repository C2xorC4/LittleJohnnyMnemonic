package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CitationHarvestState tracks which retrieval sessions have already been
// harvested so UserPromptSubmit and Stop do not double-record citations.
type CitationHarvestState struct {
	HarvestedRetrievalSessions map[string]bool `json:"harvested_retrieval_sessions"`
}

func citationHarvestStatePath(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Metrics", "citation_harvest_state.json")
}

func loadCitationHarvestState(vaultRoot string) (*CitationHarvestState, error) {
	path := citationHarvestStatePath(vaultRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CitationHarvestState{
				HarvestedRetrievalSessions: make(map[string]bool),
			}, nil
		}
		return nil, err
	}
	var st CitationHarvestState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.HarvestedRetrievalSessions == nil {
		st.HarvestedRetrievalSessions = make(map[string]bool)
	}
	return &st, nil
}

func saveCitationHarvestState(vaultRoot string, st *CitationHarvestState) error {
	path := citationHarvestStatePath(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func alreadyHarvestedRetrievalSession(vaultRoot, retrievalSessionID string) bool {
	if retrievalSessionID == "" {
		return true
	}
	st, err := loadCitationHarvestState(vaultRoot)
	if err != nil {
		return false
	}
	return st.HarvestedRetrievalSessions[retrievalSessionID]
}

func markRetrievalSessionHarvested(vaultRoot, retrievalSessionID string) error {
	if retrievalSessionID == "" {
		return nil
	}
	st, err := loadCitationHarvestState(vaultRoot)
	if err != nil {
		return err
	}
	st.HarvestedRetrievalSessions[retrievalSessionID] = true
	return saveCitationHarvestState(vaultRoot, st)
}