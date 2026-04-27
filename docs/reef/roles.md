# Reef Roles and Skills

Reef uses **roles** to classify agents and **skills** to describe their capabilities. Tasks are routed to clients whose role and skills match the task requirements.

## Built-in Roles

### `coder`

Software development agent. Writes, reviews, and modifies code.

**Default Skills:**
- `github` — Interact with GitHub (issues, PRs, repos)
- `write_file` — Create and modify files
- `exec` — Execute shell commands
- `read_file` — Read file contents
- `edit_file` — Make targeted file edits

**Typical Tasks:**
- "Write a unit test for the auth module"
- "Refactor the database layer to use connection pooling"
- "Create a Dockerfile for this service"

**Configuration:**

```json
{
  "channels": {
    "swarm": {
      "role": "coder",
      "skills": ["github", "write_file", "exec", "read_file", "edit_file"]
    }
  }
}
```

---

### `analyst`

Research and data analysis agent. Fetches information, performs web searches, and synthesizes findings.

**Default Skills:**
- `web_fetch` — Fetch and parse web pages
- `web_search` — Search the web
- `summarize` — Summarize long documents or transcripts
- `read_file` — Read local files for analysis

**Typical Tasks:**
- "Research the latest trends in RISC-V processor development"
- "Summarize this PDF and extract key findings"
- "Compare cloud pricing between AWS, GCP, and Azure"

**Configuration:**

```json
{
  "channels": {
    "swarm": {
      "role": "analyst",
      "skills": ["web_fetch", "web_search", "summarize"]
    }
  }
}
```

---

### `tester`

Quality assurance agent. Writes tests, runs test suites, and reports bugs.

**Default Skills:**
- `exec` — Run test commands
- `write_file` — Write test files
- `read_file` — Read source code to understand test targets

**Typical Tasks:**
- "Write integration tests for the payment API"
- "Run the test suite and report any failures"
- "Check code coverage and identify untested paths"

**Configuration:**

```json
{
  "channels": {
    "swarm": {
      "role": "tester",
      "skills": ["exec", "write_file", "read_file"]
    }
  }
}
```

---

## Custom Roles

You can define custom roles by creating YAML files in `skills/roles/`.

### Role YAML Schema

```yaml
name: devops
version: "1.0"
description: |
  Infrastructure and deployment automation agent.
  Manages CI/CD pipelines, Docker containers, and cloud resources.

required_skills:
  - exec
  - write_file
  - read_file

optional_skills:
  - github
  - docker

system_prompt: |
  You are a DevOps engineer. You specialize in:
  - CI/CD pipeline configuration
  - Docker and container orchestration
  - Cloud infrastructure (AWS, GCP, Azure)
  - Monitoring and alerting setup

  When given a task:
  1. Analyze the current infrastructure
  2. Propose changes with minimal disruption
  3. Implement using infrastructure-as-code when possible
  4. Include rollback procedures

constraints:
  - never_delete_production: true
  - require_confirmation_for_destructive: true

default_config:
  model: "gpt-4"
  temperature: 0.2
  max_tokens: 4000
```

### File Location

```
skills/roles/
├── coder.yaml      # built-in
├── analyst.yaml    # built-in
├── tester.yaml     # built-in
└── devops.yaml     # your custom role
```

### Loading

Custom roles are loaded automatically when the client starts. The role name is derived from the filename (without `.yaml`).

### Validation

Roles are validated on load. Required fields:

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Role identifier (must match filename) |
| `version` | Yes | Semantic version |
| `description` | Yes | Human-readable description |
| `required_skills` | Yes | Skills this role must have |
| `system_prompt` | Yes | Prompt injected for this role's AgentLoop |
| `optional_skills` | No | Additional skills this role may use |
| `constraints` | No | Safety constraints |
| `default_config` | No | Default LLM parameters |

---

## Skill Reference

### Core Skills (available to all roles)

| Skill | Description | Tools Used |
|-------|-------------|------------|
| `write_file` | Create or overwrite files | `write_file` |
| `read_file` | Read file contents | `read_file` |
| `edit_file` | Make targeted edits | `edit_file` |
| `exec` | Execute shell commands | `exec` |
| `append_file` | Append to existing files | `append_file` |

### Communication Skills

| Skill | Description | Tools Used |
|-------|-------------|------------|
| `web_fetch` | Fetch and parse web pages | `web_fetch` |
| `web_search` | Search the web | `web_search` |
| `github` | GitHub operations | `gh` CLI |
| `message` | Send chat messages | `message` |

### Specialized Skills

| Skill | Description | Tools Used |
|-------|-------------|------------|
| `summarize` | Summarize documents | `summarize` skill |
| `weather` | Weather queries | `weather` skill |
| `tts` | Text-to-speech | `tts` skill |
| `cron` | Schedule tasks | `cron` tool |

---

## Role Matching Algorithm

When a task is submitted, the scheduler finds the best client using this priority:

1. **Role match**: `client.Role == task.RequiredRole`
2. **Skill coverage**: Client must have all `task.RequiredSkills`
3. **Availability**: `client.State == "connected"` and `client.CurrentLoad < client.Capacity`
4. **Load balancing**: Choose the client with the lowest `CurrentLoad`
5. **Exclusion**: If the task previously failed on a client, exclude that client for reassignment

---

## Best Practices

### Role Design

- **Keep roles focused**: A role should have a clear, narrow responsibility
- **Minimize required skills**: Only require skills essential for the role's core function
- **Use system prompts**: Good system prompts dramatically improve task quality

### Capacity Planning

| Role | Recommended Capacity | Notes |
|------|---------------------|-------|
| `coder` | 2-3 | Code tasks can be CPU/memory intensive |
| `analyst` | 3-5 | Research tasks are often I/O bound |
| `tester` | 2-4 | Test execution can be parallelized |

### Multi-Role Clients

A single PicoClaw node can only advertise **one role**. To create a multi-role setup, run multiple client instances:

```yaml
# docker-compose.yml
services:
  coder-client:
    image: picoclaw:latest
    configs:
      - role: coder
        skills: [github, write_file, exec]

  analyst-client:
    image: picoclaw:latest
    configs:
      - role: analyst
        skills: [web_fetch, web_search]
```
