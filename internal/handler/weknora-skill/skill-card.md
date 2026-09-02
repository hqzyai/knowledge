## Description

Import documents and perform knowledge retrieval via the WeKnora API for file,
URL, and Markdown imports, hybrid retrieval, and knowledge browsing.

## Source and license

This locally downloadable variant is derived from
[`@lyingbug/weknora` 1.0.1](https://clawhub.ai/lyingbug/skills/weknora), which is
published under MIT-0. WeKnora embeds the selected API base URL in the generated
`SKILL.md`; the API key remains an environment variable.

## Known risks and mitigations

The API key can allow an agent to create, edit, and delete knowledge-base
content. Use a scoped key where possible and confirm edit and deletion targets
before executing those operations.
