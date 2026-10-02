package agent

import (
	"fmt"
	"log/slog"
	"time"

	scout7 "github.com/KTCrisis/scout7"
	"github.com/KTCrisis/scout7/mesh"
)

// Stats tracks results for a single run cycle.
type Stats struct {
	Searched  int
	Fetched   int
	Extracted int
	Produced  int
	Skipped   int
	Errors    int
}

// Run executes one full cycle: search -> filter -> fetch -> extract -> evaluate -> diagram -> store.
func Run(mc *mesh.Client, cfg *scout7.Config) (*Stats, error) {
	stats := &Stats{}

	judge := NewJudge(cfg.Judge)

	// Recall what we've already seen.
	seenNames := ListSeenNames(mc)
	slog.Info("recalled seen architectures", "count", len(seenNames))

	for _, query := range cfg.Search.Queries {
		results, err := Search(mc, query, cfg.Search.MaxResults)
		if err != nil {
			slog.Error("search failed", "query", query, "err", err)
			stats.Errors++
			continue
		}

		stats.Searched += len(results)

		for _, sr := range results {
			// Skip already processed URLs.
			if IsURLSeen(mc, sr.URL) {
				slog.Debug("skipping seen URL", "url", sr.URL)
				stats.Skipped++
				continue
			}

			// Fetch article content.
			content, err := FetchContent(mc, sr.URL)
			if err != nil {
				slog.Warn("fetch failed", "url", sr.URL, "err", err)
				stats.Errors++
				continue
			}
			stats.Fetched++

			if len(content) < 200 {
				slog.Debug("content too short, skipping", "url", sr.URL, "len", len(content))
				stats.Skipped++
				continue
			}

			// Ask the judge first: a page it drops costs no extraction. If the
			// judge fails (network, missing key), the LLM decides as before.
			var verdict *Verdict
			if judge != nil {
				v, err := judge.Judge(content, sr.Title, seenNames)
				if err != nil {
					slog.Warn("judge failed, the LLM decides", "url", sr.URL, "err", err)
				} else if !v.Keep(cfg.Judge) {
					slog.Info("judge drops the page", "url", sr.URL, "describes", v.Describes, "listing", v.Listing)
					_ = StoreResult(mc, MemoryEntry{
						URL:    sr.URL,
						Name:   sr.Title,
						Score:  0,
						Reason: fmt.Sprintf("judge %s: not an architecture (describes %.2f, listing %.2f)", v.Model, v.Describes, v.Listing),
					})
					stats.Skipped++
					continue
				} else {
					verdict = v
				}
			}

			// Extract architecture.
			arch, err := Extract(mc, cfg.Ollama.Model, content, sr.URL)
			if err != nil {
				slog.Info("no architecture found", "url", sr.URL, "err", err)
				// Store as seen so we don't retry.
				_ = StoreResult(mc, MemoryEntry{
					URL:    sr.URL,
					Name:   sr.Title,
					Score:  0,
					Reason: "no architecture extracted",
				})
				stats.Skipped++
				continue
			}
			stats.Extracted++

			// Evaluate novelty: the judge's answer when there is one, else the LLM.
			var eval *Evaluation
			if verdict != nil {
				eval = verdict.Evaluation(cfg.Judge)
			} else if eval, err = Evaluate(mc, cfg.Ollama.Model, arch, seenNames); err != nil {
				slog.Warn("evaluation failed", "name", arch.Name, "err", err)
				stats.Errors++
				continue
			}

			entry := MemoryEntry{
				URL:      sr.URL,
				Name:     arch.Name,
				Score:    eval.Score,
				Category: eval.Category,
				Patterns: arch.Patterns,
				Reason:   eval.Reason,
			}

			// Produce output if worthy: the LLM and min_novelty_score must agree;
			// the judge's novelty_min already holds the measured threshold.
			worthy := eval.DiagramIt && eval.Score >= cfg.Evaluate.MinNoveltyScore
			if verdict != nil {
				worthy = eval.DiagramIt
			}
			if worthy {
				path, err := ProduceOutput(mc, arch, cfg.Output)
				if err != nil {
					slog.Warn("output generation failed", "name", arch.Name, "format", cfg.Output.Format, "err", err)
					stats.Errors++
				} else {
					entry.OutputPath = path
					stats.Produced++
				}
			}

			// Store result.
			if err := StoreResult(mc, entry); err != nil {
				slog.Error("memory store failed", "name", arch.Name, "err", err)
				stats.Errors++
			}

			// Track for subsequent evaluations in same cycle.
			seenNames = append(seenNames, arch.Name)
		}
	}

	return stats, nil
}

// Loop runs the agent in a continuous loop with the configured interval.
func Loop(mc *mesh.Client, cfg *scout7.Config) error {
	for {
		sessionID := fmt.Sprintf("scout7-%d", time.Now().Unix())
		var err error
		if mc, err = cfg.NewMeshClient(sessionID); err != nil {
			return fmt.Errorf("mesh client: %w", err)
		}

		slog.Info("starting cycle", "session", sessionID)
		start := time.Now()

		stats, err := Run(mc, cfg)
		elapsed := time.Since(start)

		if err != nil {
			slog.Error("cycle failed", "err", err, "elapsed", elapsed)
		} else {
			slog.Info("cycle complete",
				"elapsed", elapsed,
				"searched", stats.Searched,
				"fetched", stats.Fetched,
				"extracted", stats.Extracted,
				"produced", stats.Produced,
				"skipped", stats.Skipped,
				"errors", stats.Errors,
			)
		}

		slog.Info("sleeping", "interval", cfg.Interval)
		time.Sleep(cfg.Interval)
	}
}
