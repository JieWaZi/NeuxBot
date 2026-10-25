package global

import "github.com/JieWaZi/NeuxBot/server/internal/localweb"

// LocalWeb 只在显式 Web 模式下拥有浏览器配对及会话生命周期。
var LocalWeb *localweb.Manager
