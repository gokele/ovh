//go:build !ui

package main

import "io/fs"

// 默认 build：不嵌入前端，server/web 目录可以不存在
func hasUI() bool { return false }

func webDistFS() fs.FS { return nil }
