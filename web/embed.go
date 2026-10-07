// Package web は画面（index.html）を実行ファイルに埋め込む。
// 開発中は serve -dev web で、ここのファイルを毎回ディスクから読める。
package web

import _ "embed"

//go:embed index.html
var Index []byte
