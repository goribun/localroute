package main

import "localroute/internal/service"

func windowsTrayLabels(status service.Status, busy bool) (string, string) {
	if busy {
		return "正在处理代理操作…", "正在处理…"
	}
	label, action := "代理已停止", "启动代理"
	if status.Running {
		label, action = "代理运行中 · "+status.Listen, "停止代理"
	}
	if status.LastError != "" {
		label = "代理异常 · " + status.LastError
	}
	return label, action
}
