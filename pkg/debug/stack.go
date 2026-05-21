package debug

import (
	"fmt"
	"runtime"
	"strings"
)

func Caller(depth int) string {
	if depth <= 0 {
		return ""
	}
	log := make([]string, 0, depth)
	for i := 0; i < depth; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		pcName := runtime.FuncForPC(pc).Name()
		log = append(log, fmt.Sprintf("%s   %d   %t   %s", file, line, ok, pcName))
	}

	return strings.Join(log, "\n")
}
