//go:build ui

package main

import (
	"embed"
	"io/fs"
)

// 启用方式：`cd web && npm run build` 把前端打到 server/web/，
// 然后 `cd server && go build -tags ui ./...` —— //go:embed 会把 server/web 整目录塞进二进制。
// 不加 -tags ui 时走 webembed_noui.go，二进制不含前端，纯 API。

//go:embed all:web
var webEmbed embed.FS

func hasUI() bool { return true }

func webDistFS() fs.FS {
	sub, err := fs.Sub(webEmbed, "web")
	if err != nil {
		panic("web embed: " + err.Error())
	}
	return sub
}
