package main

import (
	"math"
	"sort"
)

// retrievalLimit is GetRelatedMemories' Limit(5).
const retrievalLimit = 5

// neighborhoodDepths are the candidate-ranking depths at which a baseline pick still counts as
// "same neighborhood": it was not chosen, but it was close.
var neighborhoodDepths = []int{10, 20}

// visibleMemories returns the indexes of the memories GetRelatedMemories could return for a turn:
// the chat's own Chat-scoped memories plus User-scoped ones that are unpinned or pinned to the
// chat's personality. With timeTravel, only memories that existed when the message was sent.
func visibleMemories(t evalTurn, mems []evalMemory, timeTravel bool) []int {
	var idx []int
	for i, m := range mems {
		if timeTravel && !m.CreatedAt.Before(t.CreatedAt) {
			continue
		}
		switch m.Scope {
		case scopeChat:
			if m.ChatID != t.ChatID {
				continue
			}
		case scopeUser:
			if m.PinnedPersonalityID != [16]byte{} && m.PinnedPersonalityID != t.PersonalityID {
				continue
			}
		}
		idx = append(idx, i)
	}
	return idx
}

// ranked is one memory's place in a model's full ordering for a turn.
type ranked struct {
	Mem  int     // index into evalExport.Memories
	Dist float64 // L2 distance to the query
}

// rankAll orders every visible memory by distance to query, nearest first (ties by index so the
// order is deterministic).
func rankAll(query []float32, visible []int, vecs [][]float32) []ranked {
	out := make([]ranked, len(visible))
	for i, m := range visible {
		out[i] = ranked{Mem: m, Dist: l2(query, vecs[m])}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dist != out[j].Dist {
			return out[i].Dist < out[j].Dist
		}
		return out[i].Mem < out[j].Mem
	})
	return out
}

// picks is what retrieval returns: the first retrievalLimit of order within threshold.
func picks(order []ranked, threshold float64) []ranked {
	var out []ranked
	for _, r := range order {
		if len(out) == retrievalLimit || r.Dist > threshold {
			break
		}
		out = append(out, r)
	}
	return out
}

// rankOf maps memory index → 1-based rank in order.
func rankOf(order []ranked) map[int]int {
	m := make(map[int]int, len(order))
	for i, r := range order {
		m[r.Mem] = i + 1
	}
	return m
}

// turnComparison is one candidate's result on one turn, against the baseline.
type turnComparison struct {
	BasePicks []ranked
	CandPicks []ranked
	// BaseRankInCand is each baseline pick's rank in the candidate's full ordering (and the
	// reverse), so a disagreement reads as "reordered" (small rank) or "missed" (large rank).
	BaseRankInCand []int
	CandRankInBase []int
	Overlap        int         // |BasePicks ∩ CandPicks|
	InNeighborhood map[int]int // depth → baseline picks within the candidate's top depth
	// BaseSimOfCand / BaseSimOfBase are the mean baseline-model cosine similarity of the
	// candidate's picks and of the baseline's own picks: how good the candidate's choices look
	// when judged in the baseline model's geometry.
	BaseSimOfCand, BaseSimOfBase float64
}

func compareTurn(baseOrder, candOrder []ranked, baseThreshold, candThreshold float64) turnComparison {
	c := turnComparison{
		BasePicks:      picks(baseOrder, baseThreshold),
		CandPicks:      picks(candOrder, candThreshold),
		InNeighborhood: map[int]int{},
	}
	baseRank, candRank := rankOf(baseOrder), rankOf(candOrder)
	inCand := map[int]bool{}
	for _, r := range c.CandPicks {
		inCand[r.Mem] = true
		c.CandRankInBase = append(c.CandRankInBase, baseRank[r.Mem])
		c.BaseSimOfCand += cosineFromL2(baseOrder[baseRank[r.Mem]-1].Dist)
	}
	for _, r := range c.BasePicks {
		cr := candRank[r.Mem]
		c.BaseRankInCand = append(c.BaseRankInCand, cr)
		if inCand[r.Mem] {
			c.Overlap++
		}
		for _, d := range neighborhoodDepths {
			if cr <= d {
				c.InNeighborhood[d]++
			}
		}
		c.BaseSimOfBase += cosineFromL2(r.Dist)
	}
	if n := len(c.CandPicks); n > 0 {
		c.BaseSimOfCand /= float64(n)
	}
	if n := len(c.BasePicks); n > 0 {
		c.BaseSimOfBase /= float64(n)
	}
	return c
}

// cosineFromL2 converts an L2 distance between unit vectors to cosine similarity.
func cosineFromL2(d float64) float64 { return 1 - d*d/2 }

// l2FromCosine is the inverse of cosineFromL2.
func l2FromCosine(c float64) float64 { return math.Sqrt(math.Max(0, 2-2*c)) }

// calibrateThreshold picks the candidate L2 cutoff whose mean pick count over the sampled turns
// is closest to the baseline's at its own cutoff, so the comparison is not skewed by one model
// simply clearing the threshold more often. candOrders are the candidate's full orderings.
func calibrateThreshold(candOrders [][]ranked, baseMeanCount float64) float64 {
	var dists []float64
	for _, o := range candOrders {
		for i := 0; i < len(o) && i < retrievalLimit; i++ {
			dists = append(dists, o[i].Dist)
		}
	}
	if len(dists) == 0 || len(candOrders) == 0 {
		return 2 // the maximum L2 distance between unit vectors: no cutoff
	}
	sort.Float64s(dists)
	// meanCount(t) = (number of top-5 distances <= t) / turns, monotone in t: walk the sorted list
	// to the first t that reaches the target.
	target := baseMeanCount * float64(len(candOrders))
	best, bestErr := dists[len(dists)-1], math.Inf(1)
	for i, d := range dists {
		if err := math.Abs(float64(i+1) - target); err < bestErr {
			best, bestErr = d, err
		}
	}
	if target < 0.5 {
		return dists[0] / 2 // baseline returns almost nothing; neither should the candidate
	}
	return best
}
