package main

import (
	"strings"
	"time"
)

func gradeBenchmarkAnswer(task BenchmarkTask, answer string) BenchmarkGradeResult {
	lower := strings.ToLower(answer)
	result := BenchmarkGradeResult{
		TaskID:    task.ID,
		Threshold: task.Grading.PassThreshold,
		GradedAt:  time.Now().UTC(),
	}

	checkRequired := func(needle string) bool {
		return strings.Contains(lower, strings.ToLower(needle))
	}

	for _, s := range task.GroundTruth.RequiredSubstrings {
		if checkRequired(s) {
			result.Matched = append(result.Matched, s)
		} else {
			result.Missing = append(result.Missing, s)
		}
	}

	if len(task.GroundTruth.RequiredSubstringsAny) > 0 {
		anyHit := false
		for _, s := range task.GroundTruth.RequiredSubstringsAny {
			if checkRequired(s) {
				result.Matched = append(result.Matched, s)
				anyHit = true
				break
			}
		}
		if !anyHit {
			result.Missing = append(result.Missing, "(any of: "+strings.Join(task.GroundTruth.RequiredSubstringsAny, ", ")+")")
		}
	}

	for _, s := range task.GroundTruth.ForbiddenSubstrings {
		if checkRequired(s) {
			result.ForbiddenHit = append(result.ForbiddenHit, s)
		}
	}

	requiredCount := len(task.GroundTruth.RequiredSubstrings)
	if len(task.GroundTruth.RequiredSubstringsAny) > 0 {
		requiredCount++
	}
	matchedRequired := len(result.Matched)

	var score float64 = 1.0
	if requiredCount > 0 {
		score = float64(matchedRequired) / float64(requiredCount)
	}
	if len(result.ForbiddenHit) > 0 {
		score = 0
	}
	result.Score = score
	result.Passed = score >= task.Grading.PassThreshold && len(result.ForbiddenHit) == 0
	return result
}

func checkBenchmarkRetrieval(task BenchmarkTask, loaded []string) BenchmarkRetrieveResult {
	loadedSet := make(map[string]bool, len(loaded))
	for _, k := range loaded {
		loadedSet[strings.ToLower(k)] = true
	}

	res := BenchmarkRetrieveResult{
		TaskID: task.ID,
		Prompt: task.Prompt,
		LoadedKeys: loaded,
	}

	expectedHit := false
	for _, exp := range task.ExpectedMemoryKeys {
		if loadedSet[strings.ToLower(exp)] {
			expectedHit = true
			break
		}
	}
	res.ExpectedHit = expectedHit

	for _, forb := range task.ForbiddenMemoryKeys {
		if loadedSet[strings.ToLower(forb)] {
			res.ForbiddenLoaded = append(res.ForbiddenLoaded, forb)
		}
	}

	if !expectedHit && len(task.AcceptableMemoryKeys) > 0 {
		acceptableOnly := true
		for _, k := range loaded {
			ok := false
			for _, acc := range task.AcceptableMemoryKeys {
				if strings.EqualFold(k, acc) {
					ok = true
					break
				}
			}
			if !ok {
				acceptableOnly = false
				break
			}
		}
		res.AcceptableOnly = acceptableOnly && len(loaded) > 0
	}

	return res
}