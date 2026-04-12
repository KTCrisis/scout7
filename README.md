# scout7

Autonomous agent that monitors the agentic AI landscape, extracts architecture patterns, and generates Excalidraw diagrams.

Built on [agent-mesh](https://github.com/KTCrisis/agent-mesh) — all tool access goes through policy, tracing, and approval.

## How it works

```
Search (searxng)
  → Filter already-seen URLs (mem7)
  → Fetch article content (fetch)
  → Extract architecture (ollama/gemma4)
  → Evaluate novelty 0-10 (ollama/gemma4)
  → Generate diagram if score >= threshold (arch7)
  → Store result in memory (mem7)
  → Sleep → repeat
```

One cycle processes all configured queries, then sleeps. Use `--once` to run a single cycle and exit.

## Prerequisites

- [agent-mesh](https://github.com/KTCrisis/agent-mesh) running on `:9090` with these MCP servers connected:
  - **searxng** — web search
  - **fetch** — URL content reader
  - **ollama** — local LLM (gemma4 or similar)
  - **arch7** — Excalidraw diagram generator
  - **mem7** — persistent memory
- A `scout7` policy in agent-mesh granting access to these tools

## Install

```bash
git clone https://github.com/KTCrisis/scout7.git
cd scout7
make build
```

Requires Go 1.23+.

## Usage

```bash
# Single cycle — search, extract, evaluate, diagram, exit
make run-once

# Continuous loop (default interval: 6h)
make run

# Or directly
./scout7 --config scout7.yaml --once
```

## Config

```yaml
mesh_url: "http://localhost:9090"
agent_id: "scout7"
interval: 6h
output_dir: "./diagrams"

search:
  queries:
    - "agentic AI architecture 2026"
    - "AI agent framework design pattern"
    - "multi-agent system architecture"
  max_results: 5

evaluate:
  min_novelty_score: 7

ollama:
  model: "gemma4:e4b"
```

| Field | Description |
|-------|-------------|
| `mesh_url` | agent-mesh HTTP endpoint |
| `agent_id` | Identity for policy evaluation (`Authorization: Bearer agent:scout7`) |
| `interval` | Sleep between cycles in loop mode |
| `output_dir` | Where `.excalidraw` diagrams are written |
| `search.queries` | Search terms sent to searxng |
| `search.max_results` | Results per query |
| `evaluate.min_novelty_score` | Minimum score (0-10) to trigger diagram generation |
| `ollama.model` | Ollama model for extraction and evaluation |

## agent-mesh policy

```yaml
# policies/scout7.yaml
name: scout7
agent: "scout7"
rules:
  - tools: ["searxng.*", "fetch.*"]
    action: allow
  - tools: ["ollama.*"]
    action: allow
  - tools: ["memory.*"]
    action: allow
  - tools: ["arch7.create_diagram", "arch7.get_diagram_info"]
    action: allow
  - tools: ["arch7.modify_diagram"]
    action: deny
  - tools: ["*"]
    action: deny
```

## Architecture

```
scout7/
  cmd/scout7/main.go    entrypoint, CLI flags (--config, --once)
  agent/
    loop.go              main agent loop + Run() single cycle
    search.go            searxng search + fetch URL content
    llm.go               ollama chat helpers
    extract.go           extract architecture from article text
    evaluate.go          judge relevance, novelty, quality
    diagram.go           generate arch7 Excalidraw diagrams
    memory.go            mem7 read/write (seen URLs, store results)
  mesh/
    client.go            agent-mesh HTTP client
  config.go              YAML config loading
```

## Output

Diagrams are written to `output_dir` as `<slug>.excalidraw` — open with [Excalidraw](https://excalidraw.com) or any compatible viewer.

Results are stored in mem7 with metadata (URL, score, category, patterns) for deduplication and recall across cycles.

## License

Apache 2.0
