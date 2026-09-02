# Claw Skill

Claw Skill 是把 WeKnora 挂给 AI Agent 用的一种方式：安装之后，OpenClaw 生态里的 Agent 就能通过 WeKnora 的 REST API 往知识库里写内容、跨库检索。

Skill 托管在 ClawHub，包名 [`@lyingbug/weknora`](https://clawhub.ai/lyingbug/weknora)（MIT-0）。WeKnora 前端也提供可直接下载的本地版本；它会把当前实例的 API 地址写进 `SKILL.md`，因此通常只需配置 API Key。

## 能做什么

| 能力 | 对应接口 |
| --- | --- |
| 上传文件 | 把 PDF / Word / Excel 等文档送进知识库并自动解析向量化 |
| 导入网页 | 按 URL 抓取正文写入知识库，支持轮询解析状态 |
| 写入 Markdown | 以 Markdown 创建或编辑知识条目，适合会议记录、结构化笔记 |
| 混合检索 | 单库 `hybrid-search` 与跨库 `knowledge-search`，向量 + 关键词召回 |
| 浏览知识库 | 列出知识库与条目、查看详情 |

## 怎么配

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

两者都是「把 WeKnora 给外部 Agent 用」，选哪个取决于对方生态：

| | Claw Skill | MCP Server |
| --- | --- | --- |
| 面向 | OpenClaw / ClawHub 生态的 Agent | 支持 MCP 协议的客户端（Claude Desktop、VS Code Copilot 等） |
| 安装 | 前端下载文件夹或从 ClawHub 安装 | `pip install tencent-weknora-mcp` 或 `uvx` 运行 |
| 传输 | 直接调 REST | stdio / SSE / Streamable HTTP |
| 能力范围 | 导入、检索、浏览（5 类） | 29 个工具，另含租户、模型、会话、Agent 问答、Wiki |
| 文档 | 本篇 | [MCP 集成](../03-features/08-mcp.md) |

需要更完整的能力（跑 Agent 对话、管模型、读 Wiki）时用 MCP Server；只是想让 Agent 存取资料，Skill 更轻。

## 相关

- 凭证与能力收窄：[租户、用户与认证授权](../03-features/01-tenant-auth.md)
- 底层接口：[API 总览](../04-api/01-api-overview.md)
- 其它集成方式：[Chrome 插件](06-chrome-extension.md)、[MCP 集成](../03-features/08-mcp.md)
