package constants

// ApplicationVersion 是 Core 对外报告的语义版本。
// 发布构建通过 -ldflags -X github.com/JieWaZi/NeuxBot/server/constants.ApplicationVersion 写入与 tag 一致的 X.Y.Z。
// 本地构建使用此默认值。该符号必须是 var，链接器才能覆盖。
var ApplicationVersion = "0.1.0"
