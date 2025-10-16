package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import (
	"os"
	"strconv"
)

//
// example to show how to declare the arguments
// and reply for an RPC.
//

type ExampleArgs struct {
	X int
}

type ExampleReply struct {
	Y int
}

const ( // 任务类型
	NoTask         = iota // 无任务
	MapTaskType           // map 任务
	ReduceTaskType        // reduce 任务
)

// RPC 请求和回复结构体
type RequestArgs struct { // Worker请求任务的参数
}
type RequestReply struct { // Worker请求任务的回复
	TaskType  int      // 任务类型
	TaskId    int      // 任务 ID
	FileNames []string // 输入文件名,中间文件列表(map 任务生成，reduce 任务需要)
	FileName  string   // 输入文件名（仅 map 任务需要）
	NReduce   int      // reduce 任务数量（仅 map 任务需要）
}
type ReportArgs struct { // Worker汇报任务完成情况的参数
	TaskType int // 任务类型
	TaskId   int // 任务 ID
}
type ReportReply struct { // Worker汇报任务完成情况的回复
	// empty for now.
}

// Add your RPC definitions here.

// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the master.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
// 制作一个独特的 UNIX 域套接字名称
func masterSock() string {
	s := "/var/tmp/824-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}
