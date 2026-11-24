# Windows V5.6 实机验收

1. Ctrl+Alt+Q 退出旧版，运行 V5.6 EXE。确认不弹控制台，悬浮球为固定小圆形；拖拽只移动悬浮球，展开面板才可缩放。
2. 等待后台 OCR 自检完成，在 ChatGPT 桌面 App 中连续点击 100 个英文单词，尤其覆盖原先出现故障的第 20～70 次。记录错误及恢复情况。
3. Ctrl+Alt+L 打开日志目录。检查 startup version=5.6、ocr_init、uia_fallback、ocr_result、ocr_skipped、ocr_latency、heartbeat。
4. 英文 OCR 语言缺失时，在 Windows 语言设置中添加英文 OCR 功能，再重启。不改变权限或执行策略。
5. 检查收藏数据、离线查询、句段选择及快捷键。

自动化测试不替代 Windows 实机验收。
