# 网易云音乐元数据与歌词插件（Navidrome）

> 从网易云音乐为 Navidrome 补全歌手与专辑元数据，并提供歌词（支持翻译）。

本插件需要 **Navidrome 0.61.0 或更高版本**。

插件通过网易云音乐为你的音乐库补充歌手头像、简介、热门歌曲、相似歌手，以及专辑简介与封面，并按曲目匹配歌词。提供两种接入方式：可对接自建的 [NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi)（`proxy` 模式），也可直连网易云官方公开 API（`direct` 模式，无需额外部署服务）。

## 功能特性

- **歌手头像**：返回歌手大图与 1v1 头像，并附带网易云 CDN 缩放参数，加载更快、体积更小。
- **歌手简介**：优先取一段式综述（`briefDesc`），缺失时回退拼接分段介绍。
- **热门歌曲**：返回歌手的热门单曲列表。
- **相似歌手**：基于网易云的相似歌手推荐。
- **专辑信息**：专辑简介与网易云页面链接。
- **专辑封面**。
- **歌词**：按「歌手 + 标题」匹配曲目歌词；可选返回**中文翻译歌词**作为额外一条；自动把网易云混入 LRC 的 JSON 元信息行（作词/作曲/编曲等）还原为标准 LRC 行，避免被丢弃。
- **严格名称匹配**：仅当名称完全一致（忽略大小写）时才采用结果，避免给冷门艺人配错信息（可关闭）。
- **两种接入模式**：自建代理（`proxy`）或官方直连（`direct`）。

## 安装

### AI 安装（推荐）

嫌手动麻烦？把下面这段 prompt 交给能操作终端的 AI Agent（如 Claude Code），让它替你下载、部署并改配置：

```text
帮我给 Navidrome 安装网易云音乐插件：
1. 从 https://github.com/XBisATrouble/netease-music-plugin/releases 下载最新的 netease.ndp
2. 放进 Navidrome 插件目录(默认 <数据目录>/plugins/,先帮我确认实际路径)
3. 在配置里设 Plugins.Enabled=true,并把 netease 加入 Agents 列表
4. 重启 Navidrome 使其生效
不确定路径或部署方式(Docker/裸机/NAS)时先问我。
```

### 手动安装

1. 从 [Releases](https://github.com/XBisATrouble/netease-music-plugin/releases) 下载 `netease.ndp`。
2. 将 `netease.ndp` 放入 Navidrome 的插件目录（默认 `<数据目录>/plugins/`）。
3. 在 Navidrome 配置中启用插件（示例见下）。
4. 重启 Navidrome，并在管理界面完成插件配置。

仅开启 `Plugins.Enabled` 只是「允许加载插件」，元数据插件要真正生效，还需把 `netease` 加入 `Agents` 列表（列表顺序即优先级）。

TOML 配置示例：

```toml
[Plugins]
Enabled = true

# 把 netease 加入元数据 agent 列表(顺序即优先级)
Agents = "netease,spotify,lastfm"
```

环境变量示例：

```bash
ND_PLUGINS_ENABLED=true
ND_AGENTS=netease,spotify,lastfm
```

## 配置

在 Navidrome 的插件设置界面填写以下配置项：

| 配置项 | 说明 | 默认值 |
| --- | --- | --- |
| `api_mode` | 接入模式：`proxy`（自建 NeteaseCloudMusicApi）或 `direct`（直连官方 API） | `direct` |
| `api_url` | **`proxy` 模式必填**：NeteaseCloudMusicApi 的容器内可达地址，例如 `http://172.18.0.1:3000` | — |
| `netease_cookie` | **`direct` 模式**：获取相似歌手和歌词需要。格式：`MUSIC_U=xxx;os=pc;appver=8.9.75;` | — |
| `search_limit` | 搜索时取回的候选条数，用于名称精确匹配（1–30） | `5` |
| `strict_name_match` | 仅当名称完全一致（忽略大小写）才采用结果 | `true` |
| `enable_translated_lyrics` | 歌词若有中文翻译，作为额外一条歌词返回 | `true` |

### 两种接入模式

#### `proxy` 模式（自建代理）

部署 [NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi) 后，把 `api_mode` 设为 `proxy`，并将 `api_url` 填为该服务在 **Navidrome 容器内可达**的地址。

- 若 Navidrome 与 NeteaseCloudMusicApi 均以容器运行，注意使用容器网络可达的地址（如宿主机 `172.18.0.1` 或服务名），而非 `localhost`。
- 此模式功能最完整，且不依赖 Cookie。

#### `direct` 模式（官方直连）

无需部署任何额外服务，插件直接调用网易云官方公开 API。

- **歌手头像、简介、热门歌曲、专辑信息与封面、歌词**均可直接获取。
- **相似歌手**和**歌词翻译**依赖登录态，需在 `netease_cookie` 中填入 `MUSIC_U`。
  - 获取方式：在浏览器登录 [music.163.com](https://music.163.com)，从开发者工具的 Cookie 中复制 `MUSIC_U` 的值，按 `MUSIC_U=xxx;os=pc;appver=8.9.75;` 格式填入。
  - 未填写 Cookie 时，相似歌手会返回明确的错误提示，其余功能不受影响。

## 工作原理

### 能力（Capabilities）

插件同时实现了元数据（MetadataAgent）与歌词（Lyrics）两类能力：

| 能力 | 接口 | 数据来源 |
| --- | --- | --- |
| 歌手头像 | `ArtistImagesProvider` | 搜索歌手 → `picUrl` / `img1v1Url` |
| 歌手简介 | `ArtistBiographyProvider` | `/artist/desc`（direct 下取 `/api/v1/artist/{id}` 的 `briefDesc`） |
| 相似歌手 | `SimilarArtistsProvider` | `/simi/artist`（direct 需 Cookie） |
| 热门歌曲 | `ArtistTopSongsProvider` | `/artist/top/song`（direct 下取 `hotSongs`） |
| 专辑信息 | `AlbumInfoProvider` | `/album`（direct 下 `/api/v1/album/{id}`） |
| 专辑封面 | `AlbumImagesProvider` | 搜索专辑 → `picUrl` |
| 歌词 | `Lyrics` | `/lyric/new`（direct 下 `/api/song/lyric`） |

### 宿主服务（Host Services）

- **HTTP**：所有网易云请求经由 `host.HTTPSend`，在 WASM 沙箱内发起。
- **Config**：读取上表中的配置项。
- **Logging**：调试日志（如未命中严格匹配时的记录）。

### 查找流程

1. 按名称（或「歌手 + 标题/专辑名」）调用搜索，取回 `search_limit` 条候选。
2. 按严格名称匹配挑选目标；`strict_name_match` 关闭时回退取第一条。
3. 用目标 ID 拉取详情（简介 / 相似 / 热门 / 专辑 / 歌词）。
4. 组装为 Navidrome 期望的响应返回；无有效结果时返回 not found。

### 数据源

| 模式 | 基础端点 |
| --- | --- |
| `proxy` | 自建 NeteaseCloudMusicApi（`/search`、`/artist/desc`、`/simi/artist`、`/artist/top/song`、`/album`、`/lyric/new`） |
| `direct` | `music.163.com`、`interface3.music.163.com`、`*.music.126.net` 官方公开 API |

### 关键文件

- `main.go` — capability 注册、配置读取、查找与匹配逻辑。
- `client.go` — 双模式 HTTP 客户端与各端点调用。
- `responses.go` — 网易云 API 响应结构映射。
- `lyrics.go` — LRC 清洗：把混入的 JSON 元信息行还原为标准 LRC。
- `manifest.json` — 插件清单、权限与配置 schema。

## 构建

### 前置要求

- [TinyGo](https://tinygo.org/) `0.41.1`（推荐，用于产出 WASM）
- Go `1.26+`

### 使用 Makefile

```bash
make build     # 编译出 plugin.wasm
make package   # 打包出 netease.ndp（含 manifest.json 与 plugin.wasm）
make clean     # 清理产物
```

### 手动构建

```bash
tinygo build -opt=2 -scheduler=none -no-debug \
  -o plugin.wasm -target wasip1 -buildmode=c-shared .
zip -j netease.ndp manifest.json plugin.wasm
```

产物：

- `plugin.wasm` — 编译后的 WebAssembly 模块。
- `netease.ndp` — 可直接安装到 Navidrome 的插件包（zip 归档）。

### 自动发布

推送形如 `v1.1.0` 的 tag 会触发 [GitHub Actions](.github/workflows/release.yml)，自动构建并将 `netease.ndp` 发布到 Releases。

## 说明与限制

- **无缓存层**：插件本身不缓存结果，缓存由 Navidrome 侧负责。
- **评论功能预留未实现**：网易云 `/comment/music` 端点可用，但 Navidrome 目前没有承载「评论」的字段或界面位置，故暂不实现。
- **官方 API 稳定性**：`direct` 模式依赖网易云非官方公开接口，其可用性与 Cookie 时效性可能随官方策略变化；追求稳定可选择 `proxy` 模式。
- **逐字歌词**：网易云的逐字歌词（`yrc`）暂不返回，因 Navidrome 目前不解析逐字格式。
