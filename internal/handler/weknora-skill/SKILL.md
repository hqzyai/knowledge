---
name: weknora
description: >
  Import documents and perform knowledge retrieval via the WeKnora API. Use
  when uploading files, URLs, or Markdown to a knowledge base, searching with
  hybrid retrieval, or listing and querying knowledge-base content.
metadata: {"openclaw": {"requires": {"env": ["WEKNORA_API_KEY"]}}}
---

# WeKnora

Import and retrieve knowledge through the WeKnora REST API.

## Setup

The WeKnora API base URL is already configured in this downloaded skill. Only
the API key is required:

```bash
export WEKNORA_API_KEY="sk-your-api-key"
```

Add it to the environment used by OpenClaw. `WEKNORA_BASE_URL` is optional and
only needed when deliberately overriding the URL embedded below.

## API Configuration

Before API calls, initialize the URL and verify the API key. Stop and prompt
the user if the API key is unset.

```bash
WEKNORA_SKILL_BASE_URL={{WEKNORA_BASE_URL}}
WEKNORA_BASE_URL="${WEKNORA_BASE_URL:-$WEKNORA_SKILL_BASE_URL}"
WEKNORA_BASE_URL="${WEKNORA_BASE_URL%/}"

if [ -z "$WEKNORA_API_KEY" ]; then
  echo "Missing WeKnora API key. Set WEKNORA_API_KEY before using this skill."
  exit 1
fi
```

All requests use `X-API-Key`. Define this helper in the same shell invocation
as the API operations:

```bash
wk_api() {
  local method="$1" endpoint="$2" body="$3"
  curl -sS -X "$method" "$WEKNORA_BASE_URL/$endpoint" \
    -H "X-API-Key: $WEKNORA_API_KEY" \
    -H "Content-Type: application/json" \
    -H "X-Request-ID: $(uuidgen 2>/dev/null || date +%s)" \
    ${body:+-d "$body"}
}
```

Use `curl -F` directly for multipart file uploads.

## API Decision Table

| User intent | Endpoint | Key parameters |
| --- | --- | --- |
| List knowledge bases | `GET /knowledge-bases` | — |
| View knowledge-base details | `GET /knowledge-bases/:id` | — |
| Upload a file | `POST /knowledge-bases/:id/knowledge/file` | `file`, `enable_multimodel` |
| Import a web page | `POST /knowledge-bases/:id/knowledge/url` | `url`, `enable_multimodel` |
| Write Markdown content | `POST /knowledge-bases/:id/knowledge/manual` | `title`, `content`, `tag_id` |
| Check import progress | `GET /knowledge/:id` | inspect `parse_status` |
| Browse knowledge-base content | `GET /knowledge-bases/:id/knowledge` | `page`, `page_size`, `tag_id` |
| Edit Markdown knowledge | `PUT /knowledge/manual/:id` | `title`, `content` |
| Delete a knowledge entry | `DELETE /knowledge/:id` | — |
| Search within a knowledge base | `GET /knowledge-bases/:id/hybrid-search` | `query_text`, `match_count`, thresholds |
| Search across knowledge bases | `POST /knowledge-search` | `query`, `knowledge_base_ids` |

## Common Workflows

### Upload a file and wait for parsing

```bash
# 1. Find the target knowledge base.
wk_api GET "knowledge-bases"

# 2. Upload the file and read data.id from the response.
curl -sS -X POST "$WEKNORA_BASE_URL/knowledge-bases/<kb_id>/knowledge/file" \
  -H "X-API-Key: $WEKNORA_API_KEY" \
  -F 'file=@document.pdf' \
  -F 'enable_multimodel=true'

# 3. Poll until data.parse_status is completed or failed.
wk_api GET "knowledge/<knowledge_id>"
```

### Import a URL

```bash
wk_api POST "knowledge-bases/<kb_id>/knowledge/url" \
  '{"url":"https://example.com/article","enable_multimodel":true}'
```

Poll `knowledge/<knowledge_id>` until parsing finishes.

### Write Markdown knowledge

```bash
wk_api POST "knowledge-bases/<kb_id>/knowledge/manual" \
  '{"title":"Meeting Notes","content":"# Q1 Review\n\nKey points..."}'
```

### Search knowledge

```bash
# Single-knowledge-base hybrid search.
wk_api GET "knowledge-bases/<kb_id>/hybrid-search" \
  '{"query_text":"deployment process","match_count":5}'

# Cross-knowledge-base semantic search.
wk_api POST "knowledge-search" \
  '{"query":"deployment process","knowledge_base_ids":["kb-1","kb-2"]}'
```

### Browse knowledge-base content

```bash
wk_api GET "knowledge-bases/<kb_id>/knowledge?page=1&page_size=20"
wk_api GET "knowledge/<knowledge_id>"
```

## Response Fields

- Knowledge base: `data[]` includes `id`, `name`, `description`, `type`,
  `embedding_model_id`, `knowledge_count`, `chunk_count`, `is_processing`, and
  `created_at`.
- Knowledge entry: `data` includes `id`, `title`, `description`, `type`,
  `parse_status`, `enable_status`, `file_name`, `file_type`, `file_size`,
  `source`, `created_at`, `processed_at`, and `error_message`.
- Hybrid-search result: `data[]` includes `id`, `content`, `score`,
  `knowledge_id`, `knowledge_title`, `knowledge_filename`, `chunk_index`,
  `chunk_type`, `match_type`, and `metadata`.
- Paginated list: `data[]` plus `total`, `page`, and `page_size`.

## Status Values

- `parse_status`: `pending` -> `processing` -> `completed` or `failed`
- `enable_status`: `enabled` or `disabled`
- Knowledge `type`: `file`, `url`, or `manual`
- Knowledge-base `type`: `document` or `faq`
- `chunk_type`: `text`, `summary`, or `image`

## Operational Notes

- `GET /knowledge-bases` returns the key's personal allow-list plus any
  same-workspace knowledge bases marked workspace-visible. Those public
  knowledge bases are read-only for external-user keys; editing and deletion
  remain limited to the stable allow-list.
- `GET /knowledge-bases/:id/hybrid-search` requires a JSON request body. Pass
  it with `curl -d` even though the method is GET.
- File upload uses `multipart/form-data`, not JSON.
- A successfully parsed entry automatically changes to `enable_status=enabled`.
- If parsing fails, inspect `error_message` before retrying with
  `POST /knowledge/:id/reparse`.
- Search scores range from 0 to 1; higher values are more relevant.
- Confirm the exact target before editing or deleting knowledge.

## Error Handling

| HTTP code | Meaning | Suggested action |
| --- | --- | --- |
| `400` | Bad request | Check required fields and parameter formats |
| `401` | Unauthorized | Verify `WEKNORA_API_KEY` |
| `403` | Forbidden | Confirm access to the target resource |
| `404` | Not found | Check the resource ID |
| `413` | Payload too large | Reduce or split the file |
| `500` | Server error | Retry after a short delay |
