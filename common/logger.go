package common

import (
	"fmt"
	"log"
	"os"
)

func NewLogger(prefix string, output *os.File) *log.Logger {
	f := fmt.Sprintf("[%s] ", prefix)
	return log.New(output, f, log.LstdFlags)
}

var Logger = NewLogger("Main", os.Stdout)
