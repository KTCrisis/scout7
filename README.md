# scout7

Autonomous web research agent. Searches the web, extracts structured information, and produces output via any MCP tool.

Default use case: scout agentic AI architectures and generate Excalidraw diagrams.

Built on [agent-mesh](https://github.com/KTCrisis/agent-mesh) — all tool access goes through policy, tracing, and approval.

## How it works

```
Search (searxng)
  → Filter already-seen URLs (mem7)
  → Fetch article content (fetch)
  → Extract architecture (ollama/gemma4)
  → Evaluate novelty 0-10 (ollama/gemma4)
  → Produce output if score >= threshold (configurable tool)
  → Store result in memory (mem7)
  → Sleep → repeat
```

One cycle processes all configured queries, then sleeps. Use `--once` to run a single cycle and exit.

## Prerequisites

- [agent-mesh](https://github.com/KTCrisis/agent-mesh) running on `:9090` with these MCP servers connected:
  - **searxng** — web search
  - **fetch** — URL content reader
  - **[ollama-mcp-go](https://github.com/KTCrisis/ollama-mcp-go)** — MCP bridge to local [Ollama](https://ollama.com) (must be installed and running)
  - **[mem7](https://github.com/KTCrisis/mem7)** — persistent memory
  - An **output tool** (default: [arch7](https://github.com/KTCrisis/arch7) for Excalidraw diagrams)
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
# Single cycle — search, extract, evaluate, output, exit
make run-once

# Continuous loop (default interval: 24h)
make run

# Or directly
./scout7 --config scout7.yaml --once
```

## Config

```yaml
mesh_url: "http://localhost:9090"
agent_id: "scout7"
interval: 24h

output:
  tool: "arch7.create_diagram"
  format: "diagram"
  dir: "./diagrams"
  extension: ".excalidraw"
  params:
    direction: "LR"
    theme: "professional"

search:
  queries:
    - "agentic AI architecture 2026"
    - "AI agent framework design pattern"
    - "multi-agent system architecture"
  max_results: 3

evaluate:
  min_novelty_score: 7

ollama:
  model: "gemma4:e4b"
```

### Output formats

The `output` section controls how scout7 materializes results:

| Format | Tool | Description |
|--------|------|-------------|
| `diagram` | [`arch7`](https://github.com/KTCrisis/arch7)`.create_diagram` | Excalidraw diagrams (nodes/connections) |
| `markdown` | `filesystem.write_file` | Structured markdown reports |
| `json` | `filesystem.write_file` | Raw architecture JSON |
| `memory` | [`mem7`](https://github.com/KTCrisis/mem7)`.memory_store` | Store directly in mem7 (no file) |

### Config reference

| Field | Description |
|-------|-------------|
| `mesh_url` | agent-mesh HTTP endpoint |
| `agent_id` | Identity for policy evaluation (`Authorization: Bearer agent:scout7`) |
| `interval` | Sleep between cycles in loop mode |
| `output.tool` | MCP tool to call for output |
| `output.format` | Output format (`diagram`, `markdown`, `json`, `memory`) |
| `output.dir` | Directory for file-based outputs |
| `output.extension` | File extension |
| `output.params` | Static params passed to the output tool |
| `search.queries` | Search terms sent to searxng |
| `search.max_results` | Results per query |
| `evaluate.min_novelty_score` | Minimum score (0-10) to trigger output |
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
    output.go            pluggable output (diagram, markdown, json, memory)
    memory.go            mem7 read/write (seen URLs, store results)
  mesh/
    client.go            agent-mesh HTTP client
  config.go              YAML config loading
```

## Output

Results are written to `output.dir` as `<slug><extension>` via the configured MCP tool.

Results are also stored in mem7 with metadata (URL, score, category, patterns) for deduplication and recall across cycles.

## License

Apache 2.0
