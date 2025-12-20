package gemquick

import (
	"regexp"
	"runtime"
	"time"
)

func (g *Gemquick) LoadTime(start time.Time) {
	elapsed := time.Since(start)

	pc, _, _, _ := runtime.Caller(1)
	funcObj := runtime.FuncForPC(pc)
	funcName := regexp.MustCompile(`\.(.*)$`).ReplaceAllString(funcObj.Name(), "$1")

	g.Logging.Info.Printf("%s took %s", funcName, elapsed)
}
