# LocalRoute

LocalRoute 是面向本地开发的跨平台域名路由代理。一个可执行文件同时提供 macOS/Windows GUI 和 CLI。

## 使用

```bash
localroute                                      # GUI
localroute start --config ./localroute.yml      # CLI 前台代理
localroute check --config ./localroute.yml      # 严格校验 YAML
localroute routes --config ./localroute.yml     # 查看有效路由
localroute version
```

GUI 当前包含路由管理、嵌套条件规则、实时请求日志和监听设置。关闭窗口会隐藏应用并保持代理运行；再次启动会唤醒同一实例，可在设置页明确退出。

## YAML v2 配置

```yaml
version: 2
listener:
  address: 127.0.0.1
  port: 80
routes:
  - id: local-web
    name: 本地 Web
    group: 示例
    enabled: true
    host: web.test.example.com
    target: http://127.0.0.1:3000
    preserveHost: true
    rules:
      - id: api
        name: API 请求
        enabled: true
        priority: 100
        match:
          methods: [GET, POST]
          pathPrefix: /api
        target: http://127.0.0.1:9000
```

规则按 `priority` 从高到低匹配，相同优先级按配置顺序匹配，命中第一条后停止；没有规则命中时使用路由默认目标。`match` 必须且只能配置 `path` 或 `pathPrefix` 之一。

保存配置采用临时文件加原子替换。运行时监测文件变化，完整校验并编译新路由表后才替换；无效修改不会影响上一份有效路由。

配置查找顺序：命令行 `--config`、`LOCALROUTE_CONFIG` 环境变量、当前目录 `localroute.yml`、操作系统用户配置目录。

## 技术栈

- Go 1.26+
- Wails v2.15
- Vue 3 + TypeScript + Vite
- YAML v3

请求日志只保存在内存，最多 1000 条，不记录请求体、响应体、Cookie 或 Token。

## 开发与构建

需要 Go 1.26+、Node.js 20.19+：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
go test ./...
wails build
```

macOS 产物位于 `build/bin/LocalRoute.app`；Windows 构建生成 `build/bin/LocalRoute.exe`。GitHub Actions 会分别在 macOS 与 Windows 原生 Runner 上执行测试和打包。

LocalRoute 当前不修改系统 Hosts，开发域名需要解析到 `127.0.0.1`。默认监听 `127.0.0.1:80`；macOS GUI 在点击启动时申请管理员授权，仅以特权端口桥接进程占用 80，应用主体仍以普通用户运行。Windows 的低端口授权流程尚待完善。
