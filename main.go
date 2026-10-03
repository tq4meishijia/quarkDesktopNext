// Copyright (c) 2026 tq4meishijia
// SPDX-License-Identifier: MIT
//
// 本文件属于 quarkDesktopNext 桌面端自有代码，以 MIT 许可证授权（许可证全文见仓库根 LICENSE）。
// 注意：本工程在编译期链接 quark-cil/（AGPL-3.0）；将二者一同编译并对外分发的
// 整体产物须遵循 AGPL-3.0，详见仓库根 NOTICE。

// Command kuake-desktop 是夸克网盘的桌面端 GUI 客户端。
//
// 它不实现任何网盘协议：所有网络与文件操作都通过 app 包转发到
// github.com/zhangjingwei/kuake_cli/sdk，与 CLI 共用同一套能力。
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"kuake-desktop/app"
)

//go:embed all:frontend
var assets embed.FS

// version 是应用版本号。
//
// 编译时由发布流水线用 ldflags 注入，例如：
//
//	-ldflags "-s -w -X main.version=1.7.2"
//
// 缺省为 "dev"，表示本地开发构建。
//
// 注入后可用于三处：窗口标题、关于页展示、以及 Go 侧日志。
// **必须声明为可被 ldflags -X 寻址的包级字符串变量**，
// 否则 -X 会被静默忽略（构建照常成功，但版本号永远为空）——
// 本工程此前就踩过这个坑，见 HANDOFF §6.3 第 14 项。
var version = "dev"

func main() {
	application := app.New()
	application.SetVersion(version)

	err := wails.Run(&options.App{
		Title:     "夸克网盘桌面版 " + version,
		Width:     1240,
		Height:    800,
		MinWidth:  900,
		MinHeight: 600,
		// 窗口底色与暗色主题一致，避免启动时闪一下白屏。
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        application.Startup,
		OnDomReady:       application.DomReady,
		OnShutdown:       application.Shutdown,
		Bind:             []interface{}{application},
	})
	if err != nil {
		log.Fatal(err)
	}
}
