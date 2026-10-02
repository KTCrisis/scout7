# scout7

Autonomous web research agent. Searches the web, extracts structured information, and produces output via any MCP tool.

Default use case: scout agentic AI architectures and generate Excalidraw diagrams.

Built on [flux7-mesh](https://github.com/KTCrisis/flux7-mesh) — all tool access goes through policy, tracing, and approval.

## How it works

```
Search (searxng)
  → Filter already-seen URLs (mem7)
  → Fetch article content (fetch)
  → Judge: keep the page? how new? (Jev, optional, outside the mesh)
  → Extract architecture (ollama/gemma4)
  → Evaluate novelty: the judge's answer, or 0-10 by ollama/gemma4
  → Produce output if novel enough (configurable tool)
  → Store result in memory (mem7)
  → Sleep → repeat
```

One cycle processes all configured queries, then sleeps. Use `--once` to run a single cycle and exit.

## Prerequisites

- [flux7-mesh](https://github.com/KTCrisis/flux7-mesh) running on `:9090` with these MCP servers connected:
  - **searxng** — web search
  - **fetch** — URL content reader
  - **[ollama-mcp-go](https://github.com/KTCrisis/ollama-mcp-go)** — MCP bridge to local [Ollama](https://ollama.com) (must be installed and running)
  - **[mem7](https://github.com/KTCrisis/flux7-memory)** — persistent memory
  - An **output tool** (default: [arch7](https://github.com/KTCrisis/arch7) for Excalidraw diagrams)
- A `scout7` policy in flux7-mesh granting access to these tools

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
| `memory` | [`mem7`](https://github.com/KTCrisis/flux7-memory)`.memory_store` | Store directly in mem7 (no file) |

### Config reference

| Field | Description |
|-------|-------------|
| `mesh_url` | flux7-mesh HTTP endpoint |
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
| `ollama.model` | Ollama model for extraction (and evaluation without a judge) |
| `judge.provider` | `jev` to let a System One judge decide what to keep and draw; empty leaves it to the LLM |
| `judge.backend` | `cloudflare` (Workers AI `typesafe/jev`), `typesafe` (`jev-latest`) or `local` (Ollama `/v1/systemone`, `nimble`) |
| `judge.url` | Overrides the endpoint (a gateway in front of the model) |
| `judge.api_key_env`, `judge.account_id_env` | Names of the variables holding the key and the Cloudflare account |
| `judge.keep_min`, `judge.listing_max` | Extract a page when `describes_architecture` >= keep_min and `product_listing` < listing_max |
| `judge.novelty_min` | Draw when the novelty level (0 rehash … 3 new paradigm) reaches it |

The judge is called directly, not through the mesh: the mesh governs agents and their tools, the judge is a model. Thresholds were measured on a 30-page bench; a failed call falls back to the LLM.

## flux7-mesh policy

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
    judge.go             System One judge (Jev): keep the page, novelty 0-3
    evaluate.go          LLM novelty score 0-10 (without a judge)
    output.go            pluggable output (diagram, markdown, json, memory)
    memory.go            mem7 read/write (seen URLs, store results)
  mesh/
    client.go            flux7-mesh HTTP client
  config.go              YAML config loading
```

## Output

Results are written to `output.dir` as `<slug><extension>` via the configured MCP tool.

Results are also stored in mem7 with metadata (URL, score, category, patterns) for deduplication and recall across cycles.

## License

Apache 2.0
