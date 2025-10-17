package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/rpc"
	"os"
	"sort"
	"time"
)

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
// ihash() 生成一个非负整数
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrworker.go calls this function.
// 主循环：持续向 Master 请求任务并处理
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Your worker implementation here.
	for {
		// 向 Master 请求任务
		reply := RequestTask()

		switch reply.TaskType {
		case NoTask:
			// 无任务，休眠一段时间后继续请求
			time.Sleep(time.Second)
		case MapTaskType:
			// 处理 Map 任务
			err := handleMapTask(&reply, mapf)
			if err != nil {
				log.Printf("handleMapTask failed: %v", err)
			} else {
				ReportTaskDone(&reply)
			}
		case ReduceTaskType:
			// 处理 Reduce 任务
			err := handleReduceTask(&reply, reducef)
			if err != nil {
				log.Printf("handleReduceTask failed: %v", err)
			}
			if err == nil {
				ReportTaskDone(&reply)
			}
		}
	}

	// uncomment to send the Example RPC to the master.
	// CallExample()

}

// 向 Master 请求任务
func RequestTask() RequestReply {
	// 向 Master 发送任务请求
	args := RequestArgs{}
	reply := RequestReply{}
	ok := call("Master.AssignTask", &args, &reply)
	if !ok {
		log.Fatalf("call Master.AssignTask failed")
	}
	return reply
}

// 向 Master 汇报任务完成情况
func ReportTaskDone(reply *RequestReply) ReportReply {
	// 向 Master 发送任务完成情况
	args := ReportArgs{
		TaskType: reply.TaskType,
		TaskId:   reply.TaskId,
	}
	var r ReportReply
	ok := call("Master.ReportTaskDone", &args, &r)
	if !ok {
		log.Printf("call Master.ReportTaskDone failed")
	}
	return r
}

// 处理Map任务
// 使用“当前目录临时文件 -> 原子改名”的方式，避免部分写入
func handleMapTask(reply *RequestReply, mapf func(string, string) []KeyValue) error {
	filename := reply.FileName

	// 1) 读取输入文件
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}

	// 2) 调用用户定义的 Map 函数
	kva := mapf(filename, string(content))

	// 3) 为每个 reduce 分区准备一个临时文件和 JSON 编码器（放在当前目录）
	nReduce := reply.NReduce
	tmpFiles := make([]*os.File, nReduce)
	encoders := make([]*json.Encoder, nReduce)
	for r := 0; r < nReduce; r++ {
		// 临时文件创建在当前目录，避免写到系统临时目录
		tmp, err := os.CreateTemp(".", fmt.Sprintf("mr-%d-%d-*.tmp", reply.TaskId, r))
		if err != nil {
			// 清理已创建的临时文件
			for i := 0; i < r; i++ {
				if tmpFiles[i] != nil {
					tmpFiles[i].Close()
					os.Remove(tmpFiles[i].Name())
				}
			}
			return fmt.Errorf("create temp for partition %d: %w", r, err)
		}
		tmpFiles[r] = tmp
		encoders[r] = json.NewEncoder(tmp)
	}

	// 4) 将 Map 输出写入对应分区的临时文件（逐条 JSON）
	for _, kv := range kva {
		r := ihash(kv.Key) % nReduce
		if err := encoders[r].Encode(&kv); err != nil { // 形如{"Key": "a", "Value": 1} {"Key": "b", "Value": 1}的格式
			// 写失败：关闭并清理所有临时文件
			for i := 0; i < nReduce; i++ {
				if tmpFiles[i] != nil {
					tmpFiles[i].Close()
					os.Remove(tmpFiles[i].Name())
				}
			}
			return fmt.Errorf("encode kv to partition %d: %w", r, err)
		}
	}

	// 5) 关闭并原子改名到最终文件 mr-MapID-ReduceID
	for r := 0; r < nReduce; r++ {
		final := fmt.Sprintf("mr-%d-%d", reply.TaskId, r)
		if err := tmpFiles[r].Close(); err != nil {
			os.Remove(tmpFiles[r].Name())
			return fmt.Errorf("close tmp for %s: %w", final, err)
		}
		if err := os.Rename(tmpFiles[r].Name(), final); err != nil {
			os.Remove(tmpFiles[r].Name())
			return fmt.Errorf("rename %s -> %s: %w", tmpFiles[r].Name(), final, err)
		}
	}
	return nil
}

// 处理Reduce任务
// 返回处理错误，成功时返回 nil
func handleReduceTask(reply *RequestReply, reducef func(string, []string) string) error {
	// 读取所有中间文件，解码 JSON 数据
	intermediate := []KeyValue{}
	for _, filename := range reply.FileNames {
		file, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("open %s: %w", filename, err)
		}

		decoder := json.NewDecoder(file) // 创建 JSON 解码器
		for {
			var kv KeyValue
			err := decoder.Decode(&kv) // 逐条解码 JSON 数据
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatalf("cannot decode key-value pair from JSON")
			}
			intermediate = append(intermediate, kv)
		}
		file.Close()
	}

	// 按 key 排序
	sort.Slice(intermediate, func(i, j int) bool {
		return intermediate[i].Key < intermediate[j].Key
	})

	// 创建临时输出文件
	outFinal := fmt.Sprintf("mr-out-%d", reply.TaskId)
	outTmp, err := os.CreateTemp(".", fmt.Sprintf("mr-out-%d-*.tmp", reply.TaskId))
	if err != nil {
		return fmt.Errorf("create tmp for %s: %w", outFinal, err)
	}
	// 依次处理每个 key 相同的键值对集合
	for i := 0; i < len(intermediate); {
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		vals := make([]string, 0, j-i)
		for k := i; k < j; k++ {
			vals = append(vals, intermediate[k].Value)
		}
		fmt.Fprintf(outTmp, "%v %v\n", intermediate[i].Key, reducef(intermediate[i].Key, vals))
		i = j
	}
	// 关闭并原子改名到最终文件 mr-ReduceID
	if err := outTmp.Close(); err != nil {
		os.Remove(outTmp.Name())
		return fmt.Errorf("close tmp for %s: %w", outFinal, err)
	}
	if err := os.Rename(outTmp.Name(), outFinal); err != nil {
		os.Remove(outTmp.Name())
		return fmt.Errorf("rename %s -> %s: %w", outTmp.Name(), outFinal, err)
	}
	return nil
}

// example function to show how to make an RPC call to the master.
//
// the RPC argument and reply types are defined in rpc.go.
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	call("Master.Example", &args, &reply)

	// reply.Y should be 100.
	fmt.Printf("reply.Y %v\n", reply.Y)
}

// send an RPC request to the master, wait for the response.
// usually returns true.
// returns false if something goes wrong.
// 发送 RPC 请求到 Master，等待响应
// rpcname 是 RPC 方法名
// args 是 RPC 请求参数
// reply 是 RPC 回复参数
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := masterSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
