package raft

import (
	"fmt"
	"log"
)

// Debugging
const Debug = 1

var labNum = []int{-1, 2, 3}

func DPrintf(num int, format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		if num == labNum[0] || num == labNum[1] || num == labNum[2] {
			format = fmt.Sprintf("Lab%d: ", num) + format
			log.Printf(format, a...)
		}
	}
	return
}
