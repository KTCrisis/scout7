package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/KTCrisis/scout7/mesh"
)

// GenerateDiagram creates an Excalidraw diagram via arch7.
func GenerateDiagram(mc *mesh.Client, arch *Architecture, outputDir string) (string, error) {
	slog.Info("generating diagram", "name", arch.Name)

	nodes := make([]map[string]any, len(arch.Components))
	for i, c := range arch.Components {
		nodes[i] = map[string]any{
			"id":             c.ID,
			"label":          c.Label,
			"component_type": c.Type,
		}
	}

	connections := make([]map[string]any, len(arch.Connections))
	for i, c := range arch.Connections {
		connections[i] = map[string]any{
			"from_id": c.From,
			"to_id":   c.To,
			"label":   c.Label,
		}
	}

	outputPath := fmt.Sprintf("%s/%s.excalidraw", outputDir, slugify(arch.Name))

	tr, err := mc.CallTool("arch7.create_diagram", map[string]any{
		"nodes":       nodes,
		"connections": connections,
		"output_path": outputPath,
		"direction":   "LR",
		"theme":       "professional",
	})
	if err != nil {
		return "", fmt.Errorf("create diagram: %w", err)
	}

	// Extract the output path from the result.
	text := extractMCPText(tr.Result)
	if text != "" {
		slog.Info("diagram generated", "path", outputPath, "result", text)
	}

	return outputPath, nil
}

func formatForArch7(items []map[string]any) string {
	data, _ := json.MarshalIndent(items, "", "  ")
	return string(data)
}

func slugify(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, byte(c))
		case c >= 'A' && c <= 'Z':
			out = append(out, byte(c-'A'+'a'))
		case c == ' ' || c == '-' || c == '_':
			if len(out) > 0 && out[len(out)-1] != '-' {
				out = append(out, '-')
			}
		}
	}
	if len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "unnamed"
	}
	return string(out)
}
