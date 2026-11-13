package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// candidateResult is one candidate's comparisons, turn by turn (aligned with report.Turns).
type candidateResult struct {
	Spec       embedderSpec
	Threshold  float64
	Calibrated bool
	Turns      []turnComparison
}

type report struct {
	Options       options
	Export        *evalExport
	Turns         []sampledTurn
	NoEnrich      int
	Eligible      int
	BaseMeanPicks float64
	Results       []candidateResult
	Generated     time.Time
}

// aggregate is a candidate's summary row.
type aggregate struct {
	basePicks, candPicks, overlap, sameSet, missed int
	inNeighborhood                                 map[int]int
	baseOnlyEmpty, candOnlyEmpty                   int // turns where exactly one side returned nothing
	simCand, simBase                               float64
	simTurns                                       int
}

// missedRank is the candidate rank past which a baseline pick counts as missed entirely.
func missedRank() int { return neighborhoodDepths[len(neighborhoodDepths)-1] }

func summarize(r candidateResult) aggregate {
	a := aggregate{inNeighborhood: map[int]int{}}
	for _, t := range r.Turns {
		a.basePicks += len(t.BasePicks)
		a.candPicks += len(t.CandPicks)
		a.overlap += t.Overlap
		if t.Overlap == len(t.BasePicks) && t.Overlap == len(t.CandPicks) {
			a.sameSet++
		}
		for d, n := range t.InNeighborhood {
			a.inNeighborhood[d] += n
		}
		for _, cr := range t.BaseRankInCand {
			if cr > missedRank() {
				a.missed++
			}
		}
		switch {
		case len(t.BasePicks) > 0 && len(t.CandPicks) == 0:
			a.candOnlyEmpty++
		case len(t.BasePicks) == 0 && len(t.CandPicks) > 0:
			a.baseOnlyEmpty++
		}
		if len(t.BasePicks) > 0 && len(t.CandPicks) > 0 {
			a.simCand += t.BaseSimOfCand
			a.simBase += t.BaseSimOfBase
			a.simTurns++
		}
	}
	return a
}

func pct(n, of int) string {
	if of == 0 {
		return "–"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(of))
}

func (rep report) write(dir string) error {
	if err := rep.writeSummary(filepath.Join(dir, "summary.md")); err != nil {
		return err
	}
	if err := rep.writeDisagreements(filepath.Join(dir, "disagreements.md")); err != nil {
		return err
	}
	return rep.writeTurns(filepath.Join(dir, "turns.jsonl"))
}

func (rep report) writeSummary(path string) error {
	var b strings.Builder
	o := rep.Options
	fmt.Fprintf(&b, "# Embeddings eval\n\n%s · export `%s`\n\n", rep.Generated.Format(time.RFC3339), filepath.Base(o.exportPath))
	fmt.Fprintf(&b, "- %d memories, %d user turns in export; %d eligible after filters; sampled %d (seed %d)\n",
		len(rep.Export.Memories), len(rep.Export.Turns), rep.Eligible, len(rep.Turns)+rep.NoEnrich, o.seed)
	fmt.Fprintf(&b, "- query: `%s`", o.queryMode)
	if o.queryMode == "llm" {
		fmt.Fprintf(&b, " (%s, production memory-query prompt); %d turns skipped because the query model said no memories were needed", o.queryModel, rep.NoEnrich)
	}
	fmt.Fprintf(&b, "\n- time travel: %v (only memories created before the turn are visible)\n", o.timeTravel)
	fmt.Fprintf(&b, "- baseline: `%s`, L2 cutoff %.3f (cosine ≥ %.3f), top %d — mean %.2f picks/turn\n\n",
		baselineSpec.Name, baselineSpec.Threshold, cosineFromL2(baselineSpec.Threshold), retrievalLimit, rep.BaseMeanPicks)

	b.WriteString("| candidate | cutoff L2 (cos) | picks/turn | baseline picks kept | identical sets |")
	for _, d := range neighborhoodDepths {
		fmt.Fprintf(&b, " baseline picks in cand top %d |", d)
	}
	fmt.Fprintf(&b, " missed (rank > %d) | cand empty, base not | base empty, cand not | baseline-model cos of picks (cand / base) |\n", missedRank())
	b.WriteString("|---|---|---|---|---|")
	for range neighborhoodDepths {
		b.WriteString("---|")
	}
	b.WriteString("---|---|---|---|\n")
	n := len(rep.Turns)
	for _, r := range rep.Results {
		a := summarize(r)
		cal := ""
		if r.Calibrated {
			cal = " *cal*"
		}
		fmt.Fprintf(&b, "| `%s` | %.3f (%.3f)%s | %.2f | %s | %s |", r.Spec.Name, r.Threshold, cosineFromL2(r.Threshold), cal,
			float64(a.candPicks)/float64(max(1, n)), pct(a.overlap, a.basePicks), pct(a.sameSet, n))
		for _, d := range neighborhoodDepths {
			fmt.Fprintf(&b, " %s |", pct(a.inNeighborhood[d], a.basePicks))
		}
		sim := "–"
		if a.simTurns > 0 {
			sim = fmt.Sprintf("%.3f / %.3f", a.simCand/float64(a.simTurns), a.simBase/float64(a.simTurns))
		}
		fmt.Fprintf(&b, " %d (%s) | %d | %d | %s |\n", a.missed, pct(a.missed, a.basePicks), a.candOnlyEmpty, a.baseOnlyEmpty, sim)
	}
	b.WriteString(`
How to read it:
- *baseline picks kept*: share of the memories baseline retrieval returned that the candidate also returned.
- *in cand top N*: the "same neighborhood" bar — the candidate ranked the baseline pick within its top N even if it did not pick it.
- *missed*: baseline picks the candidate ranked past the deepest neighborhood; these are the ones to eyeball in disagreements.md.
- *cal*: the candidate's L2 cutoff was calibrated so its mean picks/turn matches the baseline's; set ` + "`threshold`" + ` in the candidates file to pin one.
- *baseline-model cos of picks*: mean baseline-model similarity of each side's picks. A candidate close to the baseline here picks memories the baseline also thinks are close, even when they are not the same ones.
`)
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func (rep report) writeDisagreements(path string) error {
	var b strings.Builder
	b.WriteString("# Disagreements\n\nWorst turns per candidate: most baseline picks missed, then least overlap. ")
	b.WriteString("Ranks are each memory's position in the other model's full ordering.\n")
	for _, r := range rep.Results {
		fmt.Fprintf(&b, "\n## %s\n", r.Spec.Name)
		order := make([]int, len(r.Turns))
		for i := range order {
			order[i] = i
		}
		missed := func(t turnComparison) int {
			n := 0
			for _, cr := range t.BaseRankInCand {
				if cr > missedRank() {
					n++
				}
			}
			return n
		}
		sort.SliceStable(order, func(i, j int) bool {
			ti, tj := r.Turns[order[i]], r.Turns[order[j]]
			if mi, mj := missed(ti), missed(tj); mi != mj {
				return mi > mj
			}
			return len(ti.BasePicks)-ti.Overlap > len(tj.BasePicks)-tj.Overlap
		})
		shown := 0
		for _, i := range order {
			t, c := rep.Turns[i], r.Turns[i]
			if shown == rep.Options.worst {
				break
			}
			if c.Overlap == len(c.BasePicks) && c.Overlap == len(c.CandPicks) {
				continue
			}
			shown++
			fmt.Fprintf(&b, "\n### %s · %s\n\n", t.CreatedAt.Format("2006-01-02"), oneLine(t.ChatName, 80))
			fmt.Fprintf(&b, "> %s\n\n**query:** %s\n\n", oneLine(t.Text, 400), oneLine(t.Query, 300))
			b.WriteString("| | baseline pick | rank in cand | |\n|---|---|---|---|\n")
			for k, p := range c.BasePicks {
				fmt.Fprintf(&b, "| %d | %s | %s | d=%.3f |\n", k+1, oneLine(rep.Export.Memories[p.Mem].Content, 220), rankLabel(c.BaseRankInCand[k]), p.Dist)
			}
			b.WriteString("\n| | candidate pick | rank in baseline | |\n|---|---|---|---|\n")
			for k, p := range c.CandPicks {
				fmt.Fprintf(&b, "| %d | %s | %s | d=%.3f |\n", k+1, oneLine(rep.Export.Memories[p.Mem].Content, 220), rankLabel(c.CandRankInBase[k]), p.Dist)
			}
		}
		if shown == 0 {
			b.WriteString("\nNo disagreements.\n")
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func rankLabel(r int) string {
	if r > missedRank() {
		return fmt.Sprintf("**%d (missed)**", r)
	}
	return fmt.Sprint(r)
}

// turnRow is one line of turns.jsonl: ids and ranks only, for ad-hoc analysis.
type turnRow struct {
	ChatID    string    `json:"chat_id"`
	CreatedAt time.Time `json:"created_at"`
	Query     string    `json:"query"`
	Visible   int       `json:"visible_memories"`
	Base      []pickRow `json:"baseline"`
	Cands     []candRow `json:"candidates"`
}

type candRow struct {
	Name  string    `json:"name"`
	Picks []pickRow `json:"picks"`
}

type pickRow struct {
	MemoryID  string  `json:"memory_id"`
	Dist      float64 `json:"dist"`
	OtherRank int     `json:"other_rank"` // rank in the other ordering (candidate for baseline picks, baseline for candidate picks)
}

func (rep report) writeTurns(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for i, t := range rep.Turns {
		row := turnRow{ChatID: t.ChatID.String(), CreatedAt: t.CreatedAt, Query: t.Query, Visible: len(t.Visible)}
		for ci, r := range rep.Results {
			c := r.Turns[i]
			if ci == 0 {
				for k, p := range c.BasePicks {
					row.Base = append(row.Base, pickRow{rep.Export.Memories[p.Mem].ID.String(), p.Dist, c.BaseRankInCand[k]})
				}
			}
			cr := candRow{Name: r.Spec.Name}
			for k, p := range c.CandPicks {
				cr.Picks = append(cr.Picks, pickRow{rep.Export.Memories[p.Mem].ID.String(), p.Dist, c.CandRankInBase[k]})
			}
			row.Cands = append(row.Cands, cr)
		}
		if err := enc.Encode(row); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func oneLine(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	if r := []rune(s); len(r) > maxRunes {
		s = string(r[:maxRunes]) + "…"
	}
	return s
}
