package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	scout7 "github.com/KTCrisis/scout7"
	"github.com/KTCrisis/scout7/mesh"
)

// ProduceOutput materializes an extracted architecture via the configured output tool.
func ProduceOutput(mc *mesh.Client, arch *Architecture, cfg scout7.OutputConfig) (string, error) {
	switch cfg.Format {
	case "diagram":
		return outputDiagram(mc, arch, cfg)
	case "markdown":
		return outputMarkdown(mc, arch, cfg)
	case "json":
		return outputJSON(mc, arch, cfg)
	case "memory":
		return outputMemory(mc, arch, cfg)
	default:
		return "", fmt.Errorf("unknown output format: %q", cfg.Format)
	}
}

// outputDiagram generates an Excalidraw diagram via arch7 (or compatible tool).
func outputDiagram(mc *mesh.Client, arch *Architecture, cfg scout7.OutputConfig) (string, error) {
	slog.Info("generating diagram", "name", arch.Name, "tool", cfg.Tool)

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

	outputPath := fmt.Sprintf("%s/%s%s", cfg.Dir, slugify(arch.Name), cfg.Extension)

	params := map[string]any{
		"nodes":       nodes,
		"connections": connections,
		"output_path": outputPath,
	}
	// Merge static params from config (direction, theme, etc.).
	for k, v := range cfg.Params {
		params[k] = v
	}

	tr, err := mc.CallTool(cfg.Tool, params)
	if err != nil {
		return "", fmt.Errorf("create diagram: %w", err)
	}

	text := extractMCPText(tr.Result)
	if text != "" {
		slog.Info("diagram generated", "path", outputPath, "result", text)
	}

	return outputPath, nil
}

// outputMarkdown writes a markdown report via a file-writing MCP tool.
func outputMarkdown(mc *mesh.Client, arch *Architecture, cfg scout7.OutputConfig) (string, error) {
	slog.Info("generating markdown", "name", arch.Name, "tool", cfg.Tool)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", arch.Name))
	sb.WriteString(fmt.Sprintf("%s\n\n", arch.Description))
	sb.WriteString(fmt.Sprintf("**Source:** %s\n\n", arch.Source))

	if len(arch.Patterns) > 0 {
		sb.WriteString("## Patterns\n\n")
		for _, p := range arch.Patterns {
			sb.WriteString(fmt.Sprintf("- %s\n", p))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Components\n\n")
	sb.WriteString("| ID | Label | Type |\n|---|---|---|\n")
	for _, c := range arch.Components {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", c.ID, c.Label, c.Type))
	}

	sb.WriteString("\n## Connections\n\n")
	sb.WriteString("| From | To | Description |\n|---|---|---|\n")
	for _, c := range arch.Connections {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", c.From, c.To, c.Label))
	}

	outputPath := fmt.Sprintf("%s/%s%s", cfg.Dir, slugify(arch.Name), cfg.Extension)

	params := map[string]any{
		"path":    outputPath,
		"content": sb.String(),
	}
	for k, v := range cfg.Params {
		params[k] = v
	}

	_, err := mc.CallTool(cfg.Tool, params)
	if err != nil {
		return "", fmt.Errorf("write markdown: %w", err)
	}

	slog.Info("markdown generated", "path", outputPath)
	return outputPath, nil
}

// outputJSON writes the raw architecture as JSON via a file-writing MCP tool.
func outputJSON(mc *mesh.Client, arch *Architecture, cfg scout7.OutputConfig) (string, error) {
	slog.Info("generating JSON", "name", arch.Name, "tool", cfg.Tool)

	data, err := json.MarshalIndent(arch, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal architecture: %w", err)
	}

	outputPath := fmt.Sprintf("%s/%s%s", cfg.Dir, slugify(arch.Name), cfg.Extension)

	params := map[string]any{
		"path":    outputPath,
		"content": string(data),
	}
	for k, v := range cfg.Params {
		params[k] = v
	}

	_, err = mc.CallTool(cfg.Tool, params)
	if err != nil {
		return "", fmt.Errorf("write JSON: %w", err)
	}

	slog.Info("JSON generated", "path", outputPath)
	return outputPath, nil
}

// outputMemory stores the architecture directly in mem7 (no file output).
func outputMemory(mc *mesh.Client, arch *Architecture, cfg scout7.OutputConfig) (string, error) {
	slog.Info("storing to memory", "name", arch.Name, "tool", cfg.Tool)

	data, err := json.Marshal(arch)
	if err != nil {
		return "", fmt.Errorf("marshal architecture: %w", err)
	}

	params := map[string]any{
		"key":   fmt.Sprintf("scout7:arch:%s", slugify(arch.Name)),
		"value": string(data),
		"agent": "scout7",
		"tags":  []string{"scout7", "architecture"},
	}
	for k, v := range cfg.Params {
		params[k] = v
	}

	_, err = mc.CallTool(cfg.Tool, params)
	if err != nil {
		return "", fmt.Errorf("store to memory: %w", err)
	}

	key := fmt.Sprintf("scout7:arch:%s", slugify(arch.Name))
	slog.Info("stored to memory", "key", key)
	return key, nil
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
