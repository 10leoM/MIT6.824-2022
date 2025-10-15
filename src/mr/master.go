package mr

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
)

const ( // 任务状态：未分配、进行中、已完成
	Idle = iota
	InProgress
	Completed
)

const ( // Master 当前任务阶段
	MapPhase    = iota // map 任务阶段
	ReducePhase        // reduce 任务阶段
	AllDone            //	所有任务完成
)

const ( // 任务类型
	NoTask         = iota // 无任务
	MapTaskType           // map 任务
	ReduceTaskType        // reduce 任务
)

type RequestArgs struct { // Worker请求任务的参数
	WorkerId int // worker ID
}

type RequestReply struct { // Worker请求任务的回复
	TaskType int // 任务类型
	TaskId   int // 任务 ID
}

type MapTask struct {
	fileName  string // 输入文件名
	mapTaskId int    // map 任务 ID
	status    int    // 任务状态：未分配、进行中、已完成
	startTime int64  // 任务开始时间
}

type ReduceTask struct {
	fileNames    []string // 中间文件列表
	reduceTaskId int      // reduce 任务 ID
	status       int      // 任务状态：未分配、进行中、已完成
	startTime    int64    // 任务开始时间
}

type Master struct { // 主节点结构体
	// Your definitions here.

	phase int        // 当前任务阶段
	mtx   sync.Mutex // 互斥锁，保护

	// map 任务相关
	mapTasks      []MapTask // map 任务列表
	nMap          int       // map 任务总数
	ncompletedMap int       // 已完成的 map 任务数量

	// reduce 任务相关
	reduceTasks      []ReduceTask // reduce 任务列表
	nReduce          int          // reduce 任务总数
	ncompletedReduce int          // 已完成的 reduce 任务数量

	// 任务调度相关

}

// Your code here -- RPC handlers for the worker to call.
// 处理来自 worker 的任务请求
func (m *Master) RequestTask(args *RequestArgs, reply *RequestReply) {
	// 互斥锁保护
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// 根据当前阶段分配任务
	switch m.phase {
	case MapPhase: // 分配 map 任务
		for i, task := range m.mapTasks {
			if task.status == Idle {
				// 分配该任务

			}
		}
		if m.ncompletedMap == nMap {
			// 切换到 reduce 阶段
			m.phase = ReducePhase
		}
	case ReducePhase: // 分配 reduce 任务
		for i, task := range m.reduceTasks {
			if task.status == Idle {

			}
		}
	case AllDone: // 所有任务完成
		reply.TaskType = NoTask
		return
	default:
		log.Fatalf("Unknown phase %v", m.phase)
	}
}

func (m *Master) ReportTaskDone(args *ReportTaskArgs, reply *ReportTaskReply) {
	// 互斥锁保护
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// 根据任务类型更新任务状态
	switch args.TaskType {
	case MapTaskType:
		if m.mapTasks[args.TaskId].status != Completed {
			m.mapTasks[args.TaskId].status = Completed
			m.ncompletedMap++
			if m.ncompletedMap == m.nMap {
				// 切换到 reduce 阶段
				m.phase = ReducePhase
			}
		}
	case ReduceTaskType:
		if m.reduceTasks[args.TaskId].status != Completed {
			m.reduceTasks[args.TaskId].status = Completed
			m.ncompletedReduce++
			if m.ncompletedReduce == m.nReduce {
				// 切换到 AllDone 阶段
				m.phase = AllDone
			}
		}
	default:
		log.Fatalf("Unknown task type %v", args.TaskType)
	}
}

// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go. Master 结构体的一个示例 RPC 处理函数方法
func (m *Master) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}

// start a thread that listens for RPCs from worker.go
// 主节点启动一个线程，监听来自 worker 的 RPC 请求
func (m *Master) server() {
	rpc.Register(m)  // 注册 RPC 服务
	rpc.HandleHTTP() // 处理 HTTP 请求
	//l, e := net.Listen("tcp", ":1234")
	sockname := masterSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrmaster.go calls Done() periodically to find out
// if the entire job has finished.
func (m *Master) Done() bool {
	// ret := false
	ret := true // 假设所有任务都完成了，立即返回
	// Your code here.

	return ret
}

// MapTask创建函数，私有
func newMapTasks(fileNames []string) []MapTask {
	fmt.Println("Creating", len(fileNames), "map tasks.")
	tasks := make([]MapTask, len(fileNames))
	for i, file := range fileNames {
		tasks[i] = MapTask{
			fileName:  file,
			mapTaskId: i,
			status:    Idle,
			startTime: 0,
		}
	}
	return tasks
}

// ReduceTask创建函数，私有
func newReduceTasks(nMap, nReduce int) []ReduceTask {
	fmt.Println("Creating", nReduce, "reduce tasks.")
	tasks := make([]ReduceTask, nReduce)
	for i := 0; i < nReduce; i++ {
		files := make([]string, 0)
		for j := 0; j < nMap; j++ {
			files = append(files, fmt.Sprintf("mr-%d-%d", j, i)) // 中间文件名为mr-X-Y，其中 X 是 map 任务 ID，Y 是 reduce 任务 ID
		}
		tasks[i] = ReduceTask{
			fileNames:    files,
			reduceTaskId: i,
			status:       Idle,
			startTime:    0,
		}
	}
}

// create a Master.
// main/mrmaster.go calls this function.
// nReduce is the number of reduce tasks to use.
// nReduce 是 reduce 任务的数量
// files 是输入文件列表
func MakeMaster(files []string, nReduce int) *Master {
	// Your code here.
	m := Master{
		phase:            MapPhase,                            // 初始阶段为 map 阶段
		mtx:              sync.Mutex{},                        // 初始化互斥锁
		mapTasks:         newMapTasks(files),                  // 创建 map 任务列表
		nMap:             len(files),                          // map 任务总数
		ncompletedMap:    0,                                   // 已完成的 map 任务数量
		reduceTasks:      newReduceTasks(len(files), nReduce), // 创建 reduce 任务列表
		nReduce:          nReduce,                             // reduce 任务总数
		ncompletedReduce: 0,                                   // 已完成的 reduce 任务数量
	}
	fmt.Println("Master created with", m.nMap, "map tasks and", m.nReduce, "reduce tasks.")

	m.server() // 启动 RPC 服务器
	return &m
}
