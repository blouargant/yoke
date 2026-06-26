# Providers & Models

The agent is provider-agnostic. A single dispatcher in `core/llm/` routes
calls to one of four backends:

| Provider         | Use for |
|---|---|
| `anthropic`      | Claude (native Anthropic API). |
| `openai`         | OpenAI's first-party API. |
| `gemini`         | Google AI Studio / Vertex Gemini. |
| `openai_compat`  | Any OpenAI-compatible endpoint (vLLM, Together, Groq, …). The default. |

## Selecting a model

Models live in their own `models.json` file (separate from `agents.json`)
with two top-level sections:

- `providers` — credentials + endpoint for each upstream API. A provider
  picks a `kind` (the four above), a `base_url`, and an `api_key`.
- `models` — model profiles. Each one points at a provider via
  `provider_ref` and inherits its credentials. Optional inline
  `provider` / `base_url` / `api_key` fields still override the inherited
  values when set.

Each agent references a model by its profile name via `model_ref` in
`registry/agents/<name>/agent.json`.

`base_url` and `api_key` are resolved as env-var names first (see
Configuration). Override leader credentials at runtime with
`OMNIS_PROVIDER`, `OMNIS_MODEL`, `OMNIS_BASE_URL`, `OMNIS_API_KEY`. The
provider-specific env vars `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`,
`GOOGLE_API_KEY` are also recognised.

A startup-time check rejects configs that still declare `models` inline
in `agents.json` — move the block to `models.json` (the loader points at
the expected path in the error message).

## Per-agent models

Different sub-agents can use different models. A common configuration:

- **leader** — top-tier reasoning model (Opus, GPT-5, Gemini 2.x Pro).
- **investigator / summariser** — smaller, faster model for evidence
  gathering and text condensation.
- **curator** — runs offline at session end; usually a mid-tier model.

The **Models** section in Settings (right after Agents) exposes every
field with inline validation. It has two sub-tabs — Providers and Models
— so credentials can be defined once and shared across model entries.

## Prompt caching

**OpenAI** and most OpenAI-compatible gateways cache automatically server-side —
no client annotation is needed, and the cache-stats plugin reports the hit rate
(read from `usage.prompt_tokens_details.cached_tokens`) in the context popup.

**Anthropic models behind a LiteLLM proxy** (reached via the `openai_compat`
provider) do **not** cache unless the request carries explicit `cache_control`
breakpoints — by Anthropic's own measure an un-annotated request shows a 0% hit
rate. Turn on the per-model **Prompt cache** toggle (Settings → Models, persisted
as `prompt_cache` in `models.json`) to have the adapter mark the long-lived
prefix (system instruction + tool catalogue) and the latest turn as cacheable;
LiteLLM forwards those breakpoints to Anthropic as `cache_control: {"type":
"ephemeral"}`. A healthy run then shows >80% hit rates after the first turn.

Leave the toggle **off** for a plain OpenAI endpoint — it caches automatically
and may reject the `cache_control` annotation. The minimum cacheable prefix is
model-dependent (≈1–4K tokens); a prefix below it is a silent no-op (the hit
rate just stays at 0), never an error.

## Web UI provider-model picker

The **Settings → Models → Models** sub-tab lists known model IDs per provider
so you can pick from a dropdown instead of typing. The ⟳ button calls
`/api/providers/models?provider_ref=<name>` on the server, which resolves the
provider's credentials and base URL from `models.json` before contacting the
upstream API — secrets never cross the wire to the browser. If the provider
returns context-length or pricing alongside each model, those fields are
auto-filled on the model entry.
