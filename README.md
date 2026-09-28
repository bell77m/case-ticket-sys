# IT Support Case Ticket System

Internal website where employees report IT problems as guests (no login) and IT staff track them to resolution.

- Frontend: SvelteKit (static SPA) · Backend: Go (net/http) + GORM · PostgreSQL · Redis
- Runs on Kubernetes (k3s) on Ubuntu Server 24.04 LTS, behind NGINX Ingress, secrets in HashiCorp Vault
- Languages: English, Chinese (Simplified), Burmese, Thai

## Docs

| File | Contents |
| --- | --- |
| [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md) | What the system must do: features, roles, fields, reports, security |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Tech stack, data model, deployment, CI/CD pipeline |
| [docs/PLAN.md](docs/PLAN.md) | Roadmap, task checklist, decisions and open questions |
| [DESIGN.md](DESIGN.md) | Design system: tokens, components, per-language typography, accessibility |
| [CLAUDE.md](CLAUDE.md) | Rules and conventions for AI coding assistants working in this repo |

Living plan (shared doc): https://claude.ai/code/artifact/d5c8225f-523b-400f-b603-74d03a28b0f7
