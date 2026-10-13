# 平台管理与系统管理员

系统管理员负责整个 WeKnora 部署的全局设置、任务队列、平台 API Key 和跨空间审计。空间 Owner 负责单个工作空间，两类身份分别授予。空间角色说明见[租户、用户与认证授权](01-tenant-auth.md)。

两类身份的管理范围如下：

| | 空间 Owner | 系统管理员 |
| --- | --- | --- |
| 作用范围 | 单个工作空间 | 整个部署 |
| 怎么获得 | 创建空间时成为 Owner，或被转让 | HQZY 初始化预置，或由现有系统管理员提升 |
| 管什么 | 空间成员、模型、知识库、集成、空间审计 | 全局系统设置、任务队列、平台 API Key、跨空间审计、重置用户密码 |
| 是否自动叠加 | — | **不会**：在某空间是 Owner 不代表是系统管理员，反之亦然 |

跨空间数据访问由 `CanAccessAllTenants` 单独控制。系统管理员身份不会自动授予其他空间的知识库内容访问权。

<Screenshot
  src="/screenshots/settings-system-admin.png"
  caption="平台控制台：系统设置、任务队列、平台 API Key 与系统审计日志"
  hint="以系统管理员身份打开「设置」，展示侧栏底部四个仅系统管理员可见的分区，正文可用系统设置页。" />

## 设置首个系统管理员 {#_1-第一个系统管理员怎么来}

当前 HQZY 初始化迁移会创建管理员 `hqzy@admin.com`（用户名 `hqzy_admin`），默认属于空间 `10000`（`HQZY System Workspace`），并授予该空间 Owner、系统管理员和跨空间访问标志。

迁移 `000094`（SQLite 为 `000016`）还会为该空间预置一把 **`HQZY Admin Full Access`** API Key。应用启动时从环境变量 **`WEKNORA_BOOTSTRAP_ADMIN_API_KEY`** 读取固定值，通过现有存储逻辑保存；配置 `SYSTEM_AES_KEY` 时加密存储。它的作用域为 `tenant`、`full_access=true`，默认不过期，可用于调用 `/api/v1/external-users`。

服务端和外部调用服务使用同一个配置值。例如，下方为说明用占位值，部署时替换为双方约定的密钥：

```dotenv
# WeKnora 服务的 .env
WEKNORA_BOOTSTRAP_ADMIN_API_KEY=sk-replace-with-your-shared-secret-at-least-32-chars
# 外部服务的配置（变量名按调用方实现调整）
WEKNORA_API_KEY=sk-replace-with-your-shared-secret-at-least-32-chars
```

外部服务将该值原样放入 `X-API-Key` 请求头。Key 必须以 `sk-` 开头，总长 32–256 字符，只允许字母、数字、`-` 和 `_`。此配置留空时跳过初始化，不会随机生成；之后补上配置并重启即可完成初始化。标准 Compose 已透传该变量，本地部署的 `env_file` 读取 `.env`，云部署读取 `.env.cloud`；Helm 可设置 `secrets.bootstrapAdminApiKey`，或在现有 Secret 中添加 `WEKNORA_BOOTSTRAP_ADMIN_API_KEY`。

相同配置重复启动保持原 Key；修改配置并重启会更新这条 Key，原值立即失效，因此调用方也要同步配置。先前版本已生成随机 Key 的部署也会更新为这里的固定值。手动撤销后的 Key 不会自动恢复。

管理员登录并切换到空间 `10000` 后，也可在「API 集成 → API Keys」中复制这把 Key，或通过 Owner JWT 调用 `GET /api/v1/tenants/10000/api-keys` 获取。初始化不会将密钥写入源码或启动日志。新安装和已有数据库升级均支持；若关闭了 `AUTO_MIGRATE`，需先执行相应数据库迁移，再启动应用。

对于没有系统管理员的部署，仍可使用 `cmd/server/bootstrap.go` 的环境变量引导流程：

1. 先用正常流程注册一个账号；
2. 为 app 服务配置 `WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL=<该账号邮箱>`，重启；
3. 启动时仅在部署尚无系统管理员的情况下，将该邮箱对应的用户设为系统管理员。

引导流程只提升已存在的账号。邮箱尚未注册时会记录告警，后续重启再次检查；配置错误不阻断服务启动。部署已有系统管理员后，该变量不再授予权限。

后续可在界面中添加或撤销系统管理员。不能撤销自己，也不能撤销最后一位系统管理员。对应接口为 `POST /system/admin/promote` 和 `/revoke`；重复撤销非管理员返回 200，审计记录以 `changed=false` 标明未发生权限变更。

## 使用平台控制台 {#_2-控制台能做什么}

系统管理员可在「设置」侧栏查看四个管理分区：

| 分区 | 作用 | 接口 |
| --- | --- | --- |
| 系统设置 | 全局运行时开关（注册模式、空间策略、并发、SSRF 白名单等），按设置项的生效规则应用，见下方设置表 | `GET/PUT/DELETE /system/admin/settings[/:key]` |
| 任务队列 | 查看 asynq 各队列实时积压、逐个任务的重试/归档/删除、批量清空归档任务；Lite 模式返回 `available=false` | `/system/admin/runtime/queues*` |
| 平台 API Key | 面向控制面自动化的 platform 作用域 Key，能力包括 `system_tenants_read/manage`、`system_settings_read/manage`、`system_runtime_read/manage`、`system_audit_read` | `/system/admin/api-keys` |
| 系统审计日志 | `tenant_id = 0` 的平台级事件（改设置、提升/撤销管理员、队列操作等）。空间级审计接口按 tenant 过滤，看不到这些行 | `GET /system/admin/audit-log` |

系统管理员还可执行以下操作：

- **重置用户密码**（`POST /system/admin/users/reset-password`）：替换目标用户的本地密码并吊销其全部会话。**不能给自己重置**——自助改密码仍要求提供旧密码；
- **批量套用默认存储配额**（`POST /system/admin/tenants/apply-default-storage-quota`）：把当前的默认配额写到所有已存在的空间上。该操作更新已有空间的配额数据。

### 创建用户

自 v0.8.2 起，系统管理员可在「设置 → 系统设置 → 账户与访问」中点击「创建用户」开通本地账号：填写用户名（2–50 字符）和邮箱，保持「自动生成随机密码」开启，或关闭后手动填写符合密码策略的密码。适用于关闭公开注册后由管理员统一开户的场景。空间分配遵循 `auth.default_tenant_mode`：`create_personal` 创建个人空间，`tenantless` 等待用户加入空间。

<Screenshot
  src="/screenshots/system-admin-create-user.png"
  caption="系统管理员创建用户：填写账号信息与一次性密码展示"
  hint="「设置 → 系统设置 → 账户与访问」下打开「创建用户」对话框，展示用户名、邮箱与「自动生成随机密码」开关；可另附创建成功后显示一次性密码和「复制账号信息」按钮的结果页。" />

自动生成的密码仅在本次创建结果显示，应当场复制完整账号信息；确认前对话框不能关闭，关闭后不能再次查询明文。每次创建都会写入系统审计日志（`system.user_created`）。重复身份返回已有用户，不改密码；邮箱和用户名分别指向不同用户时拒绝创建。接口见[系统 API](../04-api/02-api-system.md)。

创建用户与首个管理员引导是不同操作：bootstrap 仍只提升已存在用户，不负责创建账号。

## 运行时设置参考 {#_3-运行时可改的系统设置}

支持的运行时设置可在控制台中修改。数据库中的设置值优先于环境变量，多数立即生效；影响新资源或需重建运行时的设置以表中说明为准。

| 键 | 类型 | 默认 | 生效时机 |
| --- | --- | --- | --- |
| `auth.registration_mode` | `self_serve` / `invite_only` | `self_serve` | 立即 |
| `auth.default_tenant_mode` | `create_personal` / `tenantless` | `create_personal` | 只影响之后注册的新用户 |
| `tenant.self_service_creation_enabled` | bool | `true` | 立即 |
| `tenant.max_owned_per_user` | int | `10`（0 = 用内置默认，负数 = 关闭限额） | 每次建空间时读取 |
| `tenant.default_storage_quota_gb` | int | `10` | **仅新建空间时读取**，不回写已有空间 |
| `tenant.auto_create_api_key` | bool | `false` | 每次建空间时读取 |
| `ssrf.whitelist` | 字符串列表 | 空 | 立即（`SSRF_WHITELIST_EXTRA` 仍只由部署方维护，不在此覆盖） |
| `asynq.core/postprocess/enrichment/maintenance/shared/wiki_concurrency` | int | 见[异步任务系统](../02-architecture/05-async-tasks.md) | 各 worker pool 重新装配 |
| `model.max_concurrency` | int | `32` | 立即 |

以下开关同样遵循数据库优先规则：

| 键 | 默认 | 作用 |
| --- | --- | --- |
| `auth.complex_password_enabled` | false | 新注册/新密码要求大小写字母、数字和特殊字符 |
| `tenant.auto_accept_invitation` | false | 邮箱邀请已注册用户时直接写入成员关系 |
| `sandbox.docker_enabled` | false | 开放 Docker 沙箱配置和实例创建 |

`tenant.auto_create_api_key` 默认关闭。依赖旧版注册响应的集成可启用此兼容开关，使新空间自动创建 full-access Key 并返回明文。

::: tip 重置运行时设置
控制台保存设置后，修改对应环境变量不会覆盖数据库值。排查配置未生效时，先检查运行时设置；重置（`DELETE /system/admin/settings/:key`）会删除数据库覆盖值，恢复使用环境变量或内置默认值。
:::

## 相关文档 {#相关}

- 空间内的四级角色与 API Key：[租户、用户与认证授权](01-tenant-auth.md)
- 队列拓扑与 worker pool：[异步任务系统](../02-architecture/05-async-tasks.md)
- 审计日志与追踪：[可观测性与审计](16-observability.md)
- 接口清单：[系统与平台管理 API](../04-api/02-api-system.md)

## 实现参考

- `cmd/server/bootstrap.go`：首个系统管理员引导。
- `frontend/src/config/settingsAccess.ts`：平台设置分区。
- `internal/application/service/system_setting.go`：运行时设置注册表。
