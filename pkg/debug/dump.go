package debug

import (
	"github.com/davecgh/go-spew/spew"
)

//VarDump 控制台输出
func VarDump(a ...interface{}) {
	spew.Dump(a...)
}