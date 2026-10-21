# DockerSpeedTest —— Docker 镜像加速测速（免安装 Docker）

单文件 exe + 图形界面，**完全不需要安装 Docker**：直接实现 Docker Registry HTTP API v2
协议（拉镜像清单 → Bearer token 认证 → 下载镜像层计时），测量当前网络下各加速源的
真实镜像拉取速度。

**下载**：[Releases 页](https://github.com/liangd007-debug/docker-speed-test/releases/latest)
推荐下载 zip 包（引擎 + 图形界面 + 配置开箱即用）；也可单独下载 exe（GUI 与 CLI 需
成对放在同一目录，单独替换时请使用默认文件名 `DockerSpeedTest.exe` / `DockerSpeedTestGUI.exe`）。

> 首次运行 SmartScreen 可能提示"更多信息 → 仍要运行"（exe 未做代码签名）。

## 文件说明（zip 解压后，各文件放同一目录）

| 文件 | 作用 |
| --- | --- |
| `DockerSpeedTest.exe` | 测速引擎（命令行版，双击运行，结束后按回车退出） |
| `DockerSpeedTestGUI.exe` | 图形界面版（推荐，双击运行） |
| `mirrors.txt` | 镜像加速地址列表，每行一个，随改随生效（首跑自动生成默认 19 个） |
| `config.txt` | 测试参数（测试镜像、超时、并发、编码、代理等） |
| `results_日期_时间.csv` | 每次运行自动生成的结果（UTF-8 BOM，Excel 可直接打开） |

## 图形界面版（推荐）

- **左侧输入测试网站**：多行输入框直接粘贴地址（每行一个，`@url:`…`` 引用格式也认），
  点「添加」进列表；支持删除选中/清空。点「开始测试」时自动把列表写回 `mirrors.txt`。
- **顶部**：测试镜像输入框（默认 `busybox:latest`，测速想更准可换 `nginx:latest` 等大镜像）、
  ▶ 开始测试 / ■ 停止。
- **中部表格实时显示**：状态、清单延迟、下载耗时、大小、速度；**测完自动按速度排名**
  （可用的排前面，点「速度 MB/s」列头可再排序）。
- **下方共用视图（日志 / 命令二选一）**：
  - 默认显示**运行日志**（阶段1/阶段2 实时彩色输出，「运行日志」按钮绿色高亮）；
  - 点任一**复制按钮**（Linux 一键写入 / daemon.json / Windows 配置 / docker pull 前缀命令）
    = 复制到剪贴板 **并把该命令显示在下方**，按钮行从左到右：运行日志 → 4 个复制按钮；
  - 点「运行日志」按钮随时切回日志视图（历史完整重放）；
  - 双击表格行 = 快捷复制 Linux 一键写入命令。
- **底部进度条**：阶段1 连通性检测占 0–50%，阶段2 实际拉取测速占 50–100%。

GUI 调用引擎方式：`DockerSpeedTest.exe -json -nopause -list … -image …`，引擎输出
NDJSON 事件行（start/p1/p2/done/error），GUI 解析显示——也可自行对接做二次开发。

## 日志怎么读

```text
[3/19] docker.m.daocloud.io   OK   清单 1384ms, 层 1
```

- **清单 1384ms**：拉取镜像清单（manifest）耗时——连通、认证、多架构索引解析全部成功
  才会显示这个；失败则显示分类原因（超时/DNS/被拒/限流/非Registry接口…）。
- **层 1**：该镜像由 1 个镜像层组成。`busybox` 是单层构建（整根文件系统打成一个约
  2.1MB 的压缩包，即阶段2 下载的东西）；`nginx` 之类会显示「层 4」「层 6」。
  阶段2 就是按清单里列出的层逐个下载来算速度的。

## 命令行版

```text
DockerSpeedTest.exe                     # 用 config.txt + mirrors.txt
DockerSpeedTest.exe -image nginx:latest # 临时换测试镜像
DockerSpeedTest.exe -list my.txt        # 临时换列表文件
DockerSpeedTest.exe -json               # 输出 NDJSON 事件（给程序/GUI 解析）
DockerSpeedTest.exe -nocsv -nopause     # 不存 CSV、结束不等待回车
DockerSpeedTest.exe -h                  # 帮助
```

`mirrors.txt` 兼容直接粘贴聊天里的引用格式：

```text
https://docker.1ms.run
@url:`https://docker.xuanyuan.me`
url: https://docker.m.daocloud.io
```

## 两阶段测试

1. **阶段1 连通性 + 镜像清单检测**（并发 12）：请求 `/v2/library/<镜像>/manifests/<tag>`，
   走完 401 → token 认证 → 解析多架构索引得到层列表。失败原因分类显示：
   超时 / DNS解析失败 / 连接被拒 / 证书错误 / 被限流(429) / 被拒绝(403) /
   无此镜像(404) / 非Registry接口(网页) / 认证失败(带原因)。
2. **阶段2 实际拉取测速**（默认并发 1，顺序拉取互不抢带宽）：下载全部镜像层，
   按 下载字节数 ÷ 耗时 计算 MB/s，按速度从高到低排名。

## config.txt 参数

```text
image=busybox:latest     # 测试镜像，建议测速用稍大的镜像如 alpine:latest、nginx:latest
console_encoding=utf8    # 控制台编码：utf8(默认,全终端不乱码) / gbk(个别老终端) / auto(跟随原代码页)
timeout=20               # 单个请求(清单/认证)超时（秒）
download_timeout=120     # 单层下载超时（秒）
phase1_concurrency=12    # 阶段1并发数
speed_concurrency=1      # 阶段2并发数，1=顺序（速度最准），调大可缩短总时间但会互相抢带宽
min_test_mb=0            # >0 时反复下载凑够该流量，速度更稳定，如 10
save_csv=true            # 保存 CSV 结果
pause=true               # 双击运行结束后等待回车（防窗口闪退）
proxy=                   # 可选代理，如 http://127.0.0.1:10808，留空直连
```

CSV 结果样例（Excel 可直接打开）：

```text
序,镜像源,状态,说明,清单延迟ms,下载耗时s,大小Bytes,速度MB/s,层数,测试镜像,时间
1,https://docker.1panel.live,可用,层 1,2216,4.09,2226163,0.52,1,busybox:latest,2026-10-19 23:57:29
2,https://docker.xuanyuan.me,失败,被拒绝(403),,,,,0,busybox:latest,2026-10-19 23:57:29
```

## 已知情况说明

- `public.ecr.aws`、`cr.yandex` 不是 Docker Hub 镜像站，本工具按 Docker Hub 路径测
  `library/busybox`，报 **无此镜像(404)** 属正常结果，不代表站点故障。
- `dockerproxy.link`、`hubfast.cn` 等是网页工具而非 Registry 协议接口，显示
  **非Registry接口(网页)**。
- 免费加速源有 429/403 限流属常态，多跑几次结果会波动。
- 测出的速度上限受本机当时带宽影响：先看本机下载基线（如直连任意镜像站的速度），
  再横向比较各源；换个时段/网络测更有参考价值。
- 测速阶段2 默认顺序拉取（`speed_concurrency=1`），各源互不抢带宽，结果最可比。

## 从源码重新编译

```bash
# 单元测试
go test ./...

# 引擎（Go >= 1.21，依赖仅 golang.org/x/text，无 CGO）
go build -ldflags "-s -w" -o dist/DockerSpeedTest.exe .

# 图形界面（Windows 系统自带 csc；路径用反斜杠并整体加引号）
"$WINDIR/Microsoft.NET/Framework64/v4.0.30319/csc.exe" /nologo /target:winexe \
  "/out:dist\DockerSpeedTestGUI.exe" \
  /r:System.Windows.Forms.dll /r:System.Drawing.dll /r:System.Web.Extensions.dll \
  "gui\Program.cs"
```

源码结构：

```text
main.go            # 引擎：Registry v2 协议、两阶段测试、CSV、-json 事件流
console_windows.go # 控制台编码适配（默认 UTF-8 并钉 65001，可配 gbk/auto）
main_test.go       # 列表解析/镜像名拆分/参数解析单元测试
gui/Program.cs     # WinForms 图形界面（C#5，UTF-8 带 BOM）
```

控制台编码策略：默认输出 UTF-8 并把控制台代码页钉到 65001（cmd/PowerShell/
Windows Terminal/git-bash/管道捕获均不乱码）；个别老终端可在 `config.txt` 设
`console_encoding=gbk` 或 `auto`。
