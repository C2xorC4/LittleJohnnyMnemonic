package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// SCORING_ALGO_VERSION is bumped manually whenever hardcoded scoring logic
// changes that the Config hash cannot capture: the additive formula shape, the
// 0.6/0.4 tag/body split in combinedRel, the stopword / operational-term lists,
// or the Stem algorithm. Bumping it changes scoringConfigHash so before/after
// comparisons across such changes stay attributable.
//
//	1 = legacy multiplicative score (activation × relevance × confidence)
//	2 = additive ACT-R score (activation + β·relevance·confidence)
const SCORING_ALGO_VERSION = 2

// scoringConfigHash returns a stable hex SHA-256 over the scoring-relevant
// config plus SCORING_ALGO_VERSION. It is stamped onto each RetrievalSession
// (and propagated into memory_usage_log) so retrieval/usage telemetry can be
// attributed to the exact scoring algorithm + config that produced it — closing
// the confound that made the recall regression impossible to isolate.
//
// json.Marshal emits map keys in sorted order, so the hash is deterministic
// across runs despite Go's randomized map iteration.
func scoringConfigHash(cfg Config) string {
	payload := struct {
		Algo                 int                `json:"algo"`
		RetrievalThreshold   float64            `json:"retrieval_threshold"`
		MaxMemoriesLoaded    int                `json:"max_memories_loaded"`
		RelevanceWeight      float64            `json:"relevance_weight"`
		MaxActivation        float64            `json:"max_activation"`
		CitationGated        bool               `json:"citation_gated_activation"`
		SurpriseBonusWeight  float64            `json:"surprise_bonus_weight"`
		SpreadingFactor      float64            `json:"spreading_activation_factor"`
		DiscriminatingMinIDF float64            `json:"discriminating_min_idf"`
		DecayRates           map[string]float64 `json:"decay_rates"`
		ActivationFloors     map[string]float64 `json:"activation_floors"`
		EdgeWeights          map[string]float64 `json:"edge_weights"`
	}{
		Algo:                 SCORING_ALGO_VERSION,
		RetrievalThreshold:   cfg.RetrievalThreshold,
		MaxMemoriesLoaded:    cfg.MaxMemoriesLoaded,
		RelevanceWeight:      cfg.RelevanceWeight,
		MaxActivation:        cfg.MaxActivation,
		CitationGated:        cfg.CitationGatedActivation,
		SurpriseBonusWeight:  cfg.SurpriseBonusWeight,
		SpreadingFactor:      cfg.SpreadingActivationFactor,
		DiscriminatingMinIDF: DefaultDiscriminatingMinIDF,
		DecayRates:           cfg.DecayRates,
		ActivationFloors:     cfg.ActivationFloors,
		EdgeWeights:          cfg.EdgeWeights,
	}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
