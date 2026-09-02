# API 参考：租户（空间）与成员

路由注册：`internal/router/router.go` 的 `RegisterTenantRoutes`。Handler：`internal/handler/tenant.go`、`internal/handler/tenant_member.go`、`internal/handler/tenant_invitation.go`、`internal/handler/tenant_invite_link.go`、`internal/handler/audit_log.go`。

所有 `/tenants/:id/*` 路由在组级挂载 `PathTenantMatch()`（`internal/middleware/access.go`）：URL 中的 `:id` 必须等于当前活跃空间（跨空间超管例外），防止越权操作他人空间。

## 空间生命周期

### POST /api/v1/tenants

用途：创建空间（自助开新工作区；调用者自动成为 Owner）。权限：任何已登录用户（可无空间）；API key 仅平台 key 且具 `system_tenants_manage`。Handler: `internal/handler/tenant.go`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是（`binding:"required,min=1,max=128"`） | 空间名称 |
| `description` | string | 否（`binding:"max=512"`） | 描述 |

跨空间超管可提交完整 `types.Tenant`（含 `storage_quota`、`status` 等）。

响应：201 `{"success":true,"data":{Tenant}}`（配置允许时可能携带 `api_key`）。自助创建被禁用返回 403（code 2005），超配额返回 429。

```bash
curl -X POST $BASE/api/v1/tenants -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"我的空间"}'
```

### GET /api/v1/tenants

用途：列出我可访问的空间。权限：已登录；API key 需 `manage_tenant_settings` 或 full-access。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"data":{"items":[TenantResponse]}}`

```bash
curl $BASE/api/v1/tenants -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/all

用途：列出全部空间（跨空间超管）。权限：`CrossTenant()`（`CanAccessAllTenants` 且集群开启 `EnableCrossTenantAccess`）；平台 key 需 `system_tenants_read|manage`。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"data":{"items":[TenantResponse]}}`

```bash
curl $BASE/api/v1/tenants/all -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/search

用途：按关键字搜索空间（跨空间超管）。权限：同上。Handler: `internal/handler/tenant.go`

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `keyword` | string | 否 | 关键字 |
| `tenant_id` | string | 否 | 精确空间 ID |
| `page` / `page_size` | int | 否 | 分页（默认 1/20，上限 100） |

响应：200 `{"success":true,"data":{"items":[...],"total","page","page_size"}}`

```bash
curl "$BASE/api/v1/tenants/search?keyword=demo&page=1" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/:id

用途：空间详情。权限：Viewer+；平台 key 需 `system_tenants_read|manage`。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"data":{TenantResponse}}`

```bash
curl $BASE/api/v1/tenants/1 -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/tenants/:id

用途：更新空间配置。权限：Owner；平台 key 需 `system_tenants_manage`。Handler: `internal/handler/tenant.go`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | *string | 否（`binding:"omitempty,min=1,max=128"`） | 新名称 |
| `description` | *string | 否（`binding:"omitempty,max=512"`） | 新描述 |

响应：200 `{"success":true,"data":{TenantResponse}}`

```bash
curl -X PUT $BASE/api/v1/tenants/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"新名字"}'
```

### DELETE /api/v1/tenants/:id

用途：删除空间。权限：Owner；平台 key 需 `system_tenants_manage`。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"message":"Workspace deleted successfully"}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1 -H "Authorization: Bearer $TOKEN"
```

## 空间 KV 配置

`:key` 为配置键而非空间 ID（空间取自认证上下文），可选值：`web-search-config`、`prompt-templates`、`parser-engine-config`、`storage-engine-config`、`chat-history-config`、`retrieval-config`。

### GET /api/v1/tenants/kv/:key

用途：读取空间级 KV 配置。权限：Viewer+；API key 需 `manage_tenant_settings` 或 full-access。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"data":{...对应配置对象...}}`

```bash
curl $BASE/api/v1/tenants/kv/retrieval-config -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/tenants/kv/:key

用途：更新空间级 KV 配置。权限：Admin+；API key 需 `manage_tenant_settings` 或 full-access。请求体：与 `:key` 对应的配置 JSON 对象。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"message":"Configuration updated"}`

```bash
curl -X PUT $BASE/api/v1/tenants/kv/web-search-config -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"enabled":true}'
```

## API Key 与 API 主体

### POST /api/v1/external-users

用途：供 Hermes 等受信任系统同步创建用户。用户固定加入空间 `10000`，角色为 `contributor`（界面显示“编辑”）；`user_id` 同时作为 `/conversation-sync` 的外部用户 ID，使每日对话知识库归属该用户。接口返回一个能力授权 API Key：仅具有 `chat`、`retrieve`、`read_agents`，知识库权限为“预绑定的个人 Hermes 对话知识库 + 同空间 `visibility=workspace` 的公开知识库（动态只读）”。

权限：空间 `10000` 的 Owner JWT，或该空间已有的 Full Access API Key。接口不会接受其它空间的凭证。

请求：

```json
{
  "user_id": "hermes-user-123",
  "username": "hermes_user_123",
  "email": "user123@example.com",
  "password": "Hermes123456"
}
```

密码必须为 8–32 个字符并同时包含字母和数字。相同 `user_id`、用户名和邮箱的重试具有幂等性：不会重复创建账号或 API Key，并返回 HTTP 200；首次创建返回 HTTP 201。若相同 `user_id` 携带了不同用户名或邮箱，则返回 409。

响应：

```json
{
  "success": true,
  "data": {
    "external_user_id": "hermes-user-123",
    "user_id": "9f8f...",
    "username": "hermes_user_123",
    "email": "user123@example.com",
    "space_id": 10000,
    "role": "contributor",
    "api_key_id": 91,
    "api_key": "sk-...",
    "full_access": false,
    "capabilities": ["chat", "retrieve", "read_agents"],
    "knowledge_base_ids": ["..."],
    "user_created": true,
    "api_key_created": true
  }
}
```

注意：用户登录后的“编辑”权限默认进入“我创建的知识库”，并只能编辑自己创建的知识库。返回的 API Key 不能管理空间、成员、模型或知识库生命周期；`knowledge_base_ids` 仍是可写边界及稳定的个人知识库白名单，同空间公开知识库只在读取、检索和问答时动态加入。公开库改回 `personal` 后会立即失去访问权限，不需要刷新或重发 API Key。`/conversation-sync` 是例外：它只要求 API Key 有效，不检查 capability 或知识库白名单。

```bash
curl -X POST "$BASE/api/v1/external-users" \
  -H "X-API-Key: $ADMIN_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"hermes-user-123","username":"hermes_user_123","email":"user123@example.com","password":"Hermes123456"}'
```

### GET /api/v1/tenants/:id/api-keys

用途：列出空间 API key（掩码显示）。权限：Owner，仅 JWT（API key 默认拒绝）。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"data":[{id,scope_type,name,api_key(掩码),full_access,knowledge_base_ids,capabilities,last_used_at,expires_at,created_at}]}`

```bash
curl $BASE/api/v1/tenants/1/api-keys -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/api-keys

用途：创建空间 API key（明文仅返回一次）。权限：Owner，仅 JWT。Handler: `internal/handler/tenant.go`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | key 名称 |
| `full_access` | bool | 否 | 空间全权 key（默认 false） |
| `knowledge_base_ids` | []string | 否 | KB 白名单（scoped key） |
| `capabilities` | []string | 否 | capability 列表（见总览） |
| `expires_at_unix` | *int64 | 否 | 过期时间戳 |

响应：201 `{"success":true,"data":{...,"api_key":"<明文>","token":"<明文>"}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/api-keys -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ingest-bot","capabilities":["ingest","retrieve"],"knowledge_base_ids":["kb-1"]}'
```

### DELETE /api/v1/tenants/:id/api-keys/:key_id

用途：删除 API key。权限：Owner，仅 JWT。路径参数：`key_id`。

响应：200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1/api-keys/5 -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/tenants/:id/api-principal-config

用途：读取 API 外部用户主体配置。权限：Owner，仅 JWT。Handler: `internal/handler/tenant.go`

响应：200 `{"success":true,"data":{"mode":"tenant|direct|signed_token","direct_header_name","signed_token_header_name","require_direct_header","has_hmac_secret"}}`

```bash
curl $BASE/api/v1/tenants/1/api-principal-config -H "Authorization: Bearer $TOKEN"
```

### PUT /api/v1/tenants/:id/api-principal-config

用途：更新 API 外部用户主体配置。权限：Owner，仅 JWT。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `mode` | string | 是 | `tenant` / `direct` / `signed_token` |
| `require_direct_header` | bool | 否 | direct 模式是否强制 Header |
| `hmac_secret` | *string | 否 | signed_token 模式密钥（传 `***` 保留原值） |

响应：200，同 GET。

```bash
curl -X PUT $BASE/api/v1/tenants/1/api-principal-config -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"mode":"signed_token","hmac_secret":"topsecret"}'
```

### POST /api/v1/tenants/:id/api-principal-test-token

用途：签发用于测试的外部用户 JWT。权限：Owner，仅 JWT。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `external_user_id` | string | 是 | 外部用户 ID（≤128 字符） |
| `expires_in_seconds` | int | 否 | 1-3600，默认 900 |

响应：200 `{"success":true,"data":{"token","header_name","expires_in_seconds","expires_at_unix","external_user_id"}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/api-principal-test-token -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"external_user_id":"u-123"}'
```

## 成员管理（/tenants/:id/members）

Handler: `internal/handler/tenant_member.go`。API key 需 `manage_members` 或 full-access。

### GET /api/v1/tenants/:id/members

用途：成员列表。权限：Viewer+。

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `q` | string | 否 | 邮箱/用户名过滤 |
| `page` / `page_size` | int | 否 | 分页 |

响应：200 `{"success":true,"data":{"members":[{user_id,email,username,avatar,role,status,invited_by,joined_at}],"total","page","page_size"}}`

```bash
curl $BASE/api/v1/tenants/1/members -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/members

用途：直接添加成员。权限：Owner。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `email` | string | 是（`binding:"required,email"`） | 成员邮箱（须已注册） |
| `role` | string | 是（`binding:"required"`） | `owner/admin/contributor/viewer` |

响应：201 `{"success":true,"data":{成员对象}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/members -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"email":"b@ex.com","role":"contributor"}'
```

### PUT /api/v1/tenants/:id/members/:user_id

用途：修改成员角色。权限：Owner。请求体：`{"role":"admin"}`（`binding:"required"`）。

响应：200 `{"success":true}`

```bash
curl -X PUT $BASE/api/v1/tenants/1/members/u-123 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"admin"}'
```

### DELETE /api/v1/tenants/:id/members/:user_id

用途：移除成员。权限：Owner。

响应：200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1/members/u-123 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/leave

用途：退出空间（任何成员可自行退出；服务层拒绝导致空间无 Owner 的退出）。权限：Viewer+，仅 JWT。

响应：200 `{"success":true}`

```bash
curl -X POST $BASE/api/v1/tenants/1/leave -H "Authorization: Bearer $TOKEN"
```

## 空间邀请（/tenants/:id/invitations 与 invite-links）

Handler: `internal/handler/tenant_invitation.go`、`internal/handler/tenant_invite_link.go`。API key 需 `manage_members` 或 full-access。

### GET /api/v1/tenants/:id/invitations

用途：空间邀请列表。权限：Viewer+。

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `include_terminal` | bool | 否 | 包含已完结邀请 |
| `page` / `page_size` | int | 否 | 分页 |

响应：200 `{"success":true,"data":{"invitations":[{id,tenant_id,invitee_email,inviter_email,role,status,message,expires_at,is_share_link,accepted_count,...}],"total","page","page_size"}}`

```bash
curl $BASE/api/v1/tenants/1/invitations -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/invitations

用途：邀请成员（被邀请人在 `/me/invitations` 确认后才入库）。权限：Owner。

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `email` | string | 是（`binding:"required,email"`） | 被邀请邮箱 |
| `role` | string | 是（`binding:"required"`） | 授予角色 |
| `message` | string | 否 | 附言 |

响应：201 `{"success":true,"data":{TenantInvitationResponse}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/invitations -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"email":"c@ex.com","role":"viewer"}'
```

### DELETE /api/v1/tenants/:id/invitations/:inv_id

用途：撤销邀请。权限：Owner。

响应：200 `{"success":true}`

```bash
curl -X DELETE $BASE/api/v1/tenants/1/invitations/12 -H "Authorization: Bearer $TOKEN"
```

### POST /api/v1/tenants/:id/invite-links

用途：创建分享链接（多次可用的注册邀请链接）。权限：Owner。Handler: `internal/handler/tenant_invite_link.go`

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `role` | string | 是（`binding:"required"`） | 链接授予的角色 |
| `message` | string | 否 | 附言 |

响应：201 `{"success":true,"data":{id,token,invite_url,role,status,expires_at,is_share_link:true,accepted_count}}`

```bash
curl -X POST $BASE/api/v1/tenants/1/invite-links -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"role":"viewer"}'
```

## 审计日志

Handler: `internal/handler/audit_log.go`。游标分页。

### GET /api/v1/tenants/:id/audit-log

用途：空间审计日志（含被拒绝操作记录）。权限：Admin+，仅 JWT。

| 查询参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `after_id` | int | 否 | 游标（上次响应 `next_cursor`） |
| `limit` | int | 否 | 1-100，默认 50 |
| `action` | string | 否 | 按动作过滤（如 `rbac.member_added`） |
| `outcome` | string | 否 | `success` / `denied` |
| `actor` | string | 否 | 按操作者 user_id 过滤 |

响应：200 `{"success":true,"data":[AuditLog],"next_cursor":N}`

```bash
curl "$BASE/api/v1/tenants/1/audit-log?limit=50" -H "Authorization: Bearer $TOKEN"
```

### GET /api/v1/knowledge-bases/:id/activity

用途：单个 KB 的活动流（只读审计）。权限：KB 创建者 OR Admin+，且对 KB 有 read 权限；仅 JWT。查询参数同上（`after_id/limit/action/outcome/actor`）。注册于 `RegisterKnowledgeBaseActivityRoutes`。

响应：200 `{"success":true,"data":[AuditLog],"next_cursor":N}`

```bash
curl $BASE/api/v1/knowledge-bases/kb-1/activity -H "Authorization: Bearer $TOKEN"
```
