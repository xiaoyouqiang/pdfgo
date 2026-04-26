package debug

import (
	"fmt"
	"runtime"
	"strings"
)

func Caller(len int) string {
	var log = make([]string,4,4)
	for i := 0 ; i< len; i++ {
		pc, file, line, ok := runtime.Caller(i)
		pcName := runtime.FuncForPC(pc).Name() //获取函数名
		log = append(log,fmt.Sprintf("%s   %d   %t   %s",  file, line, ok, pcName))
	}

	return strings.Join(log,"\n")
}