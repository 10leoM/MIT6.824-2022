package kvraft

import (
	"log"
	"sync"
	"sync/atomic"
	"time"

	"../labgob"
	"../labrpc"
	"../raft"
)

const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}

type Op struct {
	Key       string //	键名
	Value     string //	键对应的值
	OpType    string //	操作类型，"Put" 或 "Append"
	ClientId  int64  //	客户端 ID
	RequestId int64  //	请求 ID
}

type Result struct {
	Op    Op     // 操作
	Value string //	键对应的值
	Err   Err    //	错误码
}

type KVServer struct {
	mu      sync.Mutex         // 互斥锁，保护对数据的并发访问
	me      int                // 服务器 ID
	rf      *raft.Raft         // Raft 实例
	applyCh chan raft.ApplyMsg // 应用通道，用于接收 Raft 应用的指令
	dead    int32              // set by Kill()，是否已被终止

	maxraftstate int // snapshot if log grows this big，Raft 日志最大大小，超过该值则触发快照

	// Your definitions here.
	data        map[string]string   // 键值对存储
	lastApplied map[int64]int64     // 客户端 ID 到最近已应用请求 ID 的映射
	waitCh      map[int]chan Result // 索引到等待通道的映射，用于通知客户端操作完成
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// 1. 封装 Op 请求
	op := Op{
		Key:       args.Key,
		OpType:    "Get",
		ClientId:  args.ClientId,
		RequestId: args.RequestId,
	}

	// 2. 将 Op 提交给 Raft
	index, _, isLeader := kv.rf.Start(op)
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}

	// 3. 创建等待通道，以便在 Raft 达成共识并应用后接收通知
	kv.mu.Lock()
	ch := make(chan Result, 1)
	kv.waitCh[index] = ch
	kv.mu.Unlock()

	// 4. 函数退出时清理通道
	defer func() {
		kv.mu.Lock()
		delete(kv.waitCh, index)
		kv.mu.Unlock()
	}()

	// 5. 等待 Raft 应用结果或超时
	select {
	case res := <-ch:
		// 6. 校验结果是否对应当前请求（防止 Leadership 变更导致 Log Index 被覆盖）
		if res.Op.ClientId == op.ClientId && res.Op.RequestId == op.RequestId {
			reply.Value = res.Value
			reply.Err = res.Err
		} else {
			// 如果 Index 处的 Op 不匹配，说明原来的 Op 没被提交（可能发生了 Leader 切换）
			reply.Err = ErrWrongLeader
		}
	case <-time.After(500 * time.Millisecond):
		// 超时，认为请求失败（可能是网络分区或选举导致）
		reply.Err = ErrWrongLeader
	}
}

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// 1. 封装 Op 请求
	op := Op{
		Key:       args.Key,
		Value:     args.Value,
		OpType:    args.Op,
		ClientId:  args.ClientId,
		RequestId: args.RequestId,
	}

	// 2. 将 Op 提交给 Raft
	index, _, isLeader := kv.rf.Start(op)
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}

	// 3. 创建等待通道
	kv.mu.Lock()
	ch := make(chan Result, 1)
	kv.waitCh[index] = ch
	kv.mu.Unlock()

	// 4. 清理通道
	defer func() {
		kv.mu.Lock()
		delete(kv.waitCh, index)
		kv.mu.Unlock()
	}()

	// 5. 等待结果或超时
	select {
	case res := <-ch:
		// 6. 校验 Op 身份
		if res.Op.ClientId == op.ClientId && res.Op.RequestId == op.RequestId {
			reply.Err = res.Err
		} else {
			reply.Err = ErrWrongLeader
		}
	case <-time.After(500 * time.Millisecond):
		reply.Err = ErrWrongLeader
	}
}

// the tester calls Kill() when a KVServer instance won't
// be needed again. for your convenience, we supply
// code to set rf.dead (without needing a lock),
// and a killed() method to test rf.dead in
// long-running loops. you can also add your own
// code to Kill(). you're not required to do anything
// about this, but it may be convenient (for example)
// to suppress debug output from a Kill()ed instance.
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// Your code here, if desired.
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

// servers[] contains the ports of the set of
// servers that will cooperate via Raft to
// form the fault-tolerant key/value service.
// me is the index of the current server in servers[].
// the k/v server should store snapshots through the underlying Raft
// implementation, which should call persister.SaveStateAndSnapshot() to
// atomically save the Raft state along with the snapshot.
// the k/v server should snapshot when Raft's saved state exceeds maxraftstate bytes,
// in order to allow Raft to garbage-collect its log. if maxraftstate is -1,
// you don't need to snapshot.
// StartKVServer() must return quickly, so it should start goroutines
// for any long-running work.
// 开始一个新的 KVServer 实例，入口函数
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	// 1. 注册 Op 结构体，以便 Raft 能正确序列化和反序列化日志条目
	labgob.Register(Op{})

	// 2. 创建 KVServer 实例
	kv := new(KVServer)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// You may need initialization code here.
	kv.data = make(map[string]string)
	kv.lastApplied = make(map[int64]int64)
	kv.waitCh = make(map[int]chan Result)

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)

	go kv.applier()

	return kv
}

func (kv *KVServer) applier() {
	for !kv.killed() {
		// 1. 从 applyCh 读取 Raft 提交的日志
		msg := <-kv.applyCh
		if msg.CommandValid {
			kv.mu.Lock()
			op := msg.Command.(Op)

			// Duplicate detection for Put/Append
			// For Get, we don't strictly need to deduplicate execution as it is read-only, but we apply it to get value
			// 2. 重复请求检测 (Duplicate Detection)
			// 只有 RequestId 大于最后记录的 ID 才执行写操作
			// Get 操作是只读的，可以重复执行（为了简化线性一致性检查，这里都统一处理）
			if op.OpType == "Get" {
				// Read only, no state change, no ID check needed for state update
			} else {
				if op.RequestId > kv.lastApplied[op.ClientId] {
					// 3. 应用操作到状态机
					if op.OpType == "Put" {
						kv.data[op.Key] = op.Value
					} else if op.OpType == "Append" {
						kv.data[op.Key] += op.Value
					}
					// 更新该 Client 的最新 RequestId
					kv.lastApplied[op.ClientId] = op.RequestId
				}
			}

			// Prepare result
			// 4. 准备结果
			var res Result
			res.Op = op
			res.Err = OK

			if op.OpType == "Get" {
				val, ok := kv.data[op.Key]
				if ok {
					res.Value = val
				} else {
					res.Err = ErrNoKey
				}
			}

			// Notify waiting RPC handler
			// 非阻塞发送，防止 handler 已经超时离开导致 applier 阻塞
			// 5. 通知正在等待的 RPC 处理器
			// 注意：这里只会通知那些通过 Start() 并注册了 waitCh 的请求
			if ch, ok := kv.waitCh[msg.CommandIndex]; ok {
				// Non-blocking send to avoid deadlock if handler timed out
				select {
				case ch <- res:
				default:
				}
			}

			kv.mu.Unlock()
		}
	}
}
