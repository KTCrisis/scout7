package agent

import (
	"fmt"
	"log/slog"

	"github.com/KTCrisis/scout7/mesh"
)

// Evaluation is the LLM's judgment on an architecture's novelty.
type Evaluation struct {
	Score     int    `json:"score"`      // 0-10
	Reason    string `json:"reason"`     // why this score
	Category  string `json:"category"`   // e.g. "framework", "pattern", "infrastructure", "research"
	DiagramIt bool   `json:"diagram_it"` // worth generating a diagram?
}

const evaluatePrompt = `You are an expert on AI agent architectures. You track what's novel vs rehashed.
Given an extracted architecture and a list of previously seen architectures, score the novelty.

Return ONLY a JSON object:
{
  "score": 0-10,
  "reason": "one sentence explaining the score",
  "category": "framework|pattern|infrastructure|research|product",
  "diagram_it": true/false
}

Scoring guide:
- 0-3: rehash of known patterns (LangChain wrapper, basic RAG, standard ReAct)
- 4-6: interesting combination or minor innovation on known patterns
- 7-8: novel approach worth documenting (new patterns, unique architecture decisions)
- 9-10: genuinely new paradigm or breakthrough architecture

Set diagram_it=true only for score >= 7 OR if the architecture has an unusual topology worth visualizing.`

// Evaluate scores an architecture for novelty against previously seen ones.
func Evaluate(mc *mesh.Client, model string, arch *Architecture, seenNames []string) (*Evaluation, error) {
	slog.Info("evaluating", "name", arch.Name)

	seenCtx := "None yet."
	if len(seenNames) > 0 {
		seenCtx = fmt.Sprintf("Previously seen architectures: %v", seenNames)
	}

	eval, err := ChatJSON[Evaluation](mc, model, []ChatMessage{
		{Role: "system", Content: evaluatePrompt},
		{Role: "user", Content: fmt.Sprintf(
			"Architecture to evaluate:\nName: %s\nDescription: %s\nComponents: %d\nPatterns: %v\n\n%s",
			arch.Name, arch.Description, len(arch.Components), arch.Patterns, seenCtx,
		)},
	})
	if err != nil {
		return nil, fmt.Errorf("evaluate: %w", err)
	}

	slog.Info("evaluated", "name", arch.Name, "score", eval.Score, "diagram", eval.DiagramIt, "reason", eval.Reason)
	return &eval, nil
}
