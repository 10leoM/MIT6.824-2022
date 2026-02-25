package raft

import (
	"fmt"
	"log"
)

// Debugging
const Debug = 1

var labNum = []rune{'A', 'B', 'C'}

func DPrintf(num rune, format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		if num == labNum[0] || num == labNum[1] {
			format = fmt.Sprintf("Lab%c: ", num) + format
			log.Printf(format, a...)
		}
	}
	return
}
