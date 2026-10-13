# Claw Skill

Claw Skill 为 OpenClaw 生态中的智能体提供 WeKnora 接入能力，通过 REST API 上传文档、导入网页和执行跨库检索。

Skill 托管在 ClawHub，包名 [`@lyingbug/weknora`](https://clawhub.ai/lyingbug/weknora)（MIT-0）。WeKnora 前端也提供可直接下载的本地版本；它会把当前实例的 API 地址写进 `SKILL.md`，因此通常只需配置 API Key。

## 主要功能 {#能做什么}

| 能力 | 对应接口 |
| --- | --- |
| 上传文件 | 把 PDF / Word / Excel 等文档送进知识库并自动解析向量化 |
| 导入网页 | 按 URL 抓取正文写入知识库，支持轮询解析状态 |
| 写入 Markdown | 以 Markdown 创建或编辑知识条目，适合会议记录、结构化笔记 |
| 混合检索 | 单库 `hybrid-search` 与跨库 `knowledge-search`，向量 + 关键词召回 |
| 浏览知识库 | 列出知识库与条目、查看详情 |

## 安装与连接 {#怎么配}

WeKnora 界面里有引导页：「设置 → 集成 → Claw Skill」。推荐使用页面上的「下载 Skill 文件夹」，步骤如下：

1. **拿 API 凭证**：「设置 → API 信息」里创建或复制 API Key；
2. **下载并安装**：下载得到 `weknora-skill.zip`，解压后安装其中的文件夹：

   ```bash
   unzip weknora-skill.zip
   openclaw skills install ./weknora
   ```

   下载接口会把页面当前使用的 API 地址写入 Skill。直接调用接口且不传 `base_url` 时，默认写入容器服务地址 `http://app:8080/api/v1`。

3. **配置 API Key**：在 OpenClaw 运行环境中设置：

   ```bash
   export WEKNORA_API_KEY=sk-xxxxx
   ```

   `WEKNORA_BASE_URL` 不再是必需变量；只有需要临时覆盖 Skill 内地址时才设置它。

4. **验证**：让 Agent 列一次知识库或跑一次检索，确认凭证与网络可达。

仍可从 ClawHub 远程安装，但 ClawHub 当前版本需要同时配置 `WEKNORA_BASE_URL` 与 `WEKNORA_API_KEY`。

## 和 MCP 的关系

Claw Skill 和 MCP Server 均可供外部智能体调用，选择取决于客户端支持的接入方式及所需功能：

| | Claw Skill | MCP Server（内置） |
| --- | --- | --- |
| 面向 | OpenClaw / ClawHub 生态的 Agent | 支持 MCP 协议的客户端（Claude Desktop、Cursor、Claude Code、VS Code Copilot 等） |
| 安装 | ClawHub 安装 Skill | 无需额外部署：在「设置 → 发布集成 → MCP Server」创建端点，客户端连接 `/mcp/<endpoint_id>` |
| 认证 | 空间 API Key（`WEKNORA_API_KEY`） | 每个端点独立令牌，可轮换、停用 |
| 传输 | 直接调 REST | Streamable HTTP；仅支持 stdio 的客户端通过 `mcp-remote` 桥接 |
| 能力范围 | 导入、检索、浏览（5 类） | 按端点勾选：检索与阅读、问答（端点默认 Agent）、Wiki、写入；可限定知识库范围 |
| 文档 | 本篇 | [MCP 集成](../03-features/08-mcp.md) |

需要问答、Wiki 工具，或希望按端点限定工具和知识库范围时，使用 MCP Server；在 OpenClaw 生态中导入、检索和浏览资料可使用 Claw Skill。仓库 `mcp-server/` 下的 Python MCP 服务已弃用，新接入请使用内置 MCP Server。

## 相关文档 {#相关}

- 凭证与能力收窄：[租户、用户与认证授权](../03-features/01-tenant-auth.md)
- 底层接口：[API 总览](../04-api/01-api-overview.md)
- 其它集成方式：[Chrome 插件](06-chrome-extension.md)、[MCP 集成](../03-features/08-mcp.md)
