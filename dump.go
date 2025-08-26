package pantryagent

import (
	"fmt"
	"runtime"

	"github.com/davecgh/go-spew/spew"
)

func Dump(v ...any) {
	_, file, line, _ := runtime.Caller(1)
	args := append([]any{fmt.Sprintf("%s:%d:", file, line)}, v...)
	spew.Dump(args...)
}
