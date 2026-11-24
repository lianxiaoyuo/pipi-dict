# Pipi Dict V5.6.1 — 划词翻译滚动绘制修复

Windows 10/11 x64 GUI 单文件 EXE。先退出旧版（Ctrl+Alt+Q），再双击新版。不要删除 `%APPDATA%\HoverDict`：SQLite 生词本及本地词库沿用旧版。建议备份该目录。

## V5.6.1

仅修复划词翻译文本框滚动时的残影与文字重叠。UIA、OCR、取词调度、SQLite、联网和生词本沿用 V5.6，未修改。

## V5.6

- 根据用户 V5.4 日志定位到 UIA `text_pattern_unavailable` 与 `word_range_unavailable`，正常 UIA 快速路径保留；失败时增加有限父控件回退及 Windows 内置英文 OCR。
- OCR 仅用于普通单击，且仅限 UIA 已验证为非密码/非编辑/非按钮的文本或文档控件，拒绝未验证控件。局部截图最大 384×108 物理像素，限制在已验证的控件及目标窗口内；使用鼠标命中的词框，不猜测相邻词。截图不落盘、不上传、不写入日志。原有单词在线查询功能仍可能将识别出的单个英文词发送给免费翻译服务。
- OCR 使用系统 Windows PowerShell 5.1 隐藏常驻进程调用 Windows.Media.Ocr，不执行外部脚本、不改变执行策略、不下载依赖。系统需安装英语 OCR 语言能力，启动后会以内置合成测试图片检查 OCR 解码识别链路；失败时仍可用 UIA 和复制查词。保留进程超时隔离和有限队列，避免无限积压。连续查词的实际识别率及延迟需要 Windows 实机验收。
- 固定小圆形悬浮球保持 V5.5 的椭圆区域、透明色键及 DWM 边框处理，禁止缩放；仅主面板可拖拽调整尺寸。程序 PE 子系统为 GUI，辅助进程设置 CREATE_NO_WINDOW 和 HideWindow。

## 快捷键

- Ctrl+Alt+Q：退出；Ctrl+Alt+D：打开面板。
- Ctrl+Alt+W：复制单词后查词；Ctrl+Alt+T：复制句段后翻译。
- Ctrl+Alt+L：直接打开日志目录。日志 `%APPDATA%\HoverDict\logs\hoverdict.log`。

重点查看 `startup version=5.6.1`、`ocr_init`（`ok` 表示内置识别自检成功）、`uia_fallback`、`ocr_skipped`、`ocr_result`、`ocr_latency`、`heartbeat`。若日志为 `ocr_language_missing`，在 Windows 设置 → 时间和语言 → 语言和区域中添加英语及其 OCR/光学字符识别语言功能，退出并重新启动软件。无需 CMD 或管理员命令。

## 编译与验证

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-H windowsgui -s -w -buildid=' -o PipiDict-V5.6.1-Windows-x64.exe ./windows
```

Linux 构建环境可运行 Go 通用测试、静态检查及 Windows 交叉编译，不能实际执行 Windows OCR、COM、鼠标钩子或验证圆形窗口外观。不能以 Go 测试通过推定 Windows 点词成功率。
