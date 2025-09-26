# 6.824 Lab Code Structure (Auto-generated)

本文件为当前工作区源码的结构化概览，聚焦各模块职责、核心数据结构、关键函数接口以及未实现(TODO)点，便于后续分阶段补全实现。

## 总览

模块 | 目录 | 作用 | 典型后续实验
----|------|------|-------------
RPC 框架 | `labrpc/` | 模拟不可靠网络的 RPC 层 | 所有实验基础
GOB 编解码 | `labgob/` | 序列化/反序列化工具 | 持久化 / Snapshot
MapReduce | `mr/`, `mrapps/`, `main/mr*` | Lab1: MapReduce 主/Worker & App | Lab1
Raft 共识 | `raft/` | Lab2: Raft 算法骨架 | Lab2A/B/C
KV 服务 (单组) | `kvraft/` | Raft 上构建线性一致 K/V | Lab3
分片 Master | `shardmaster/` | 管理 shard -> group 映射 | Lab4A
分片 KV | `shardkv/` | 多组协同的分片 K/V | Lab4B
线性化检查 | `porcupine/` | 算法执行线性化验证 | 可选测试
工具/模型 | `models/` | K/V 模型描述 | Porcupine 辅助

---
## 目录与文件细节

### 1. `labrpc/`
- `labrpc.go`: 提供 `ClientEnd`、`Server`、`Call` 机制；模拟消息丢失/延迟。Raft/kv 使用。
- `test_test.go`: 自测网络行为。

### 2. `labgob/`
- `labgob.go`: 提供 Register/Encoder/Decoder。持久化状态 & Snapshot 序列化。

### 3. MapReduce (`mr/`, `mrapps/`, `main/`)
核心目标：实现简化版 MapReduce 流程（任务调度、Worker 拉取任务、Map 输出拆分、Reduce 聚合）。

#### 关键结构与接口
`mr/rpc.go` (未展开读取但标准 6.824 提供)：
- `ExampleArgs`, `ExampleReply` 用作 RPC 示例。
- 可能需要新增：`TaskRequest/TaskReply`, `ReportArgs` 用于 Worker 申请/上报任务。

`mr/master.go`:
```
 type Master struct {
     // TODO: 需要添加
     // files []string          输入文件列表
     // nReduce int             reduce 任务数
     // mapTasks []TaskMeta     map 任务状态 (Idle, InProgress, Done)
     // reduceTasks []TaskMeta  reduce 任务状态
     // mu sync.Mutex
     // phase enum {MapPhase, ReducePhase, Done}
 }
 func (m *Master) server() // 注册 RPC & 监听 Unix Socket
 func (m *Master) Example(...)
 func (m *Master) Done() bool // 供 main/mrmaster.go 轮询
 func MakeMaster(files []string, nReduce int) *Master
```

`mr/worker.go`:
```
 type KeyValue struct {Key, Value string}
 func Worker(mapf, reducef) // 主循环：RPC 请求任务；执行；中间文件分桶；报告完成
 func ihash(key string) int // 为 key 选择 reduce 分区
 func call(rpcname string, args, reply interface{}) bool // 统一 RPC 调用
```

`mrapps/*.go`:
- `wc.go`, `indexer.go` 等：用户提供的 Map/Reduce 函数样例；`crash.go`/`mtiming.go`/`rtiming.go` 用于测试容错与性能。

`main/mrmaster.go`, `main/mrworker.go`, `main/mrsequential.go`: 启动不同运行模式（分布式 / 顺序执行 / 测试脚本）。

#### MapReduce TODO 列表 (Lab1)
- [ ] 设计 `TaskType` 和任务状态枚举
- [ ] Master 任务调度与超时重发 (task lease ~10s)
- [ ] Worker 生成中间文件：`mr-MAPID-REDUCEID` 命名，使用 JSON/GOB
- [ ] Reduce 阶段排序或桶聚合 Key -> Values
- [ ] 正确阶段转换 & `Done()` 返回
- [ ] 清理/忽略旧 RPC (例如 late report)

### 4. Raft (`raft/`)
`raft.go` 是骨架，需要实现 Figure 2 所定义：
```
 type Raft struct {
     mu sync.Mutex
     peers []*labrpc.ClientEnd
     persister *Persister
     me int
     dead int32
     // Persistent state on all servers:
     // currentTerm int
     // votedFor int
     // log []LogEntry {Term, Command}
     // Volatile state on all servers:
     // commitIndex, lastApplied int
     // Volatile state on leaders:
     // nextIndex []int
     // matchIndex []int
 }
 type ApplyMsg {CommandValid bool; Command interface{}; CommandIndex int}
 type RequestVoteArgs {Term, CandidateId, LastLogIndex, LastLogTerm}
 type RequestVoteReply {Term, VoteGranted bool}
 func (rf *Raft) GetState() (term int, isLeader bool)
 func (rf *Raft) persist()
 func (rf *Raft) readPersist(data []byte)
 func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply)
 func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool
 func (rf *Raft) Start(command interface{}) (index, term int, isLeader bool)
 func (rf *Raft) Kill()
 func Make(peers []*labrpc.ClientEnd, me int, persister *Persister, applyCh chan ApplyMsg) *Raft
```
还需新增：
- AppendEntries RPC 定义与实现
- 选举定时器/心跳 goroutine
- 日志提交推进 & applyCh 投递
- Snapshot (Lab2C) 与 InstallSnapshot RPC

`persister.go`: 持久化字节数组存储 (RaftState / Snapshot)。
`config.go`, `test_test.go`: 测试框架辅助。

#### Raft TODO 分阶段
Lab2A (Leader 选举)
- [ ] 定时器随机超时 -> Candidate
- [ ] 投票规则 + 任期(Term)递增
- [ ] RequestVote RPC + 安全条件 (last log term/index)
- [ ] 成为 Leader 后立刻心跳

Lab2B (日志复制)
- [ ] AppendEntries RPC + 日志匹配/回退
- [ ] 提交规则 (多数 matchIndex)
- [ ] 应用状态机 applyCh

Lab2C (持久化 + 快照)
- [ ] persist()/readPersist() 编码 currentTerm/votedFor/log
- [ ] Snapshot 触发条件
- [ ] InstallSnapshot RPC

### 5. KV (单组 `kvraft/`)
```
 type Op struct { // 需要: Type(string), Key, Value, ClientId, Seq }
 type KVServer struct {
     mu sync.Mutex
     me int
     rf *raft.Raft
     applyCh chan raft.ApplyMsg
     dead int32
     maxraftstate int
     // store map[string]string
     // lastSeq map[ClientId]Seq  去重
     // waitCh map[int]chan ApplyMsg  log index -> waiter
 }
 func (kv *KVServer) Get(args *GetArgs, reply *GetReply)
 func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply)
 func StartKVServer(...)
```
#### KV TODO
- [ ] Clerk 端幂等 Seq 设计（client.go 已给模板）
- [ ] Start() 后阻塞等待日志提交 or 超时重试
- [ ] 线性化保证：对未成为 Leader 要返回 ErrWrongLeader
- [ ] Raft 日志膨胀 -> snapshot 触发 (maxraftstate)
- [ ] applyCh 协调、重复命令过滤

### 6. ShardMaster (`shardmaster/`)
维护 `Config` 列表；处理 Join/Leave/Move/Query 操作，通过 Raft 共识线性序列化。
```
 type ShardMaster struct {
     mu sync.Mutex
     me int
     rf *raft.Raft
     applyCh chan raft.ApplyMsg
     configs []Config // 历史配置
 }
 type Op {Type string; Args ...}
 Join/Leave/Move/Query -> 组成 Op -> Raft.Start -> 等待 apply
```
Rebalance 策略：尽量均衡 shard 数量；最少移动；Group 删除后迁移。

### 7. ShardKV (`shardkv/`)
基于 ShardMaster 提供的配置进行分片拉取/迁移。
```
 type ShardKV struct {
     mu sync.Mutex
     me int; rf *raft.Raft; applyCh chan raft.ApplyMsg
     make_end func(string)*labrpc.ClientEnd
     gid int; masters []*labrpc.ClientEnd
     maxraftstate int
     // shards [NShards]ShardData
     // config shardmaster.Config (current)
     // prevConfig shardmaster.Config
     // inFlight migrations
     // client dedup map
 }
 Op: {Type: Get/Put/Append/Config/ShardInstall/ShardGC, ...}
```
#### ShardKV TODO
- [ ] 周期性向 ShardMaster 拉取新配置 (只有 Leader)
- [ ] 新配置生效前迁移数据：拉取所有需要的分片
- [ ] ShardInstall 流程通过 Raft 应用
- [ ] 垃圾回收确认 (ShardGC)
- [ ] 与 snapshot 结合

### 8. Porcupine (`porcupine/`)
线性化检查工具，`checker.go` 实现模型检查；`model.go` 定义操作、状态转换；`visualization.go` 辅助图形/统计。

### 9. `models/kv.go`
k/v 的 Porcupine 模型定义：操作类型、应用逻辑，用于一致性验证。

---
## 接口与数据结构建议汇总

### 通用辅助
```
func (rf *Raft) IsLeader() bool { term, isL := rf.GetState(); return isL }
```

### Raft 日志条目
```
type LogEntry struct {
    Term    int
    Command interface{}
}
```

### AppendEntries RPC (需新增)
```
type AppendEntriesArgs struct {
    Term, LeaderId int
    PrevLogIndex, PrevLogTerm int
    Entries []LogEntry
    LeaderCommit int
}

type AppendEntriesReply struct {
    Term int
    Success bool
    // 可选优化: ConflictTerm, ConflictIndex 用于快速回退
}
```

### KV RPC (client/server)
```
type GetArgs struct {Key string; ClientId int64; Seq int}
type GetReply struct {Err Err; Value string}

type PutAppendArgs struct {Key, Value, Op string; ClientId int64; Seq int}
type PutAppendReply struct {Err Err}
```
Err 枚举：`OK, ErrNoKey, ErrWrongLeader, ErrTimeout`

---
## 并发 / 时序 关键点
- Raft 内部所有共享状态需 `rf.mu` 保护
- Apply goroutine: 监听 commitIndex 推进，向 `applyCh` 发送
- KV/ShardKV 等待日志提交时使用 index->channel map；需防止泄漏
- 选举与心跳定时器使用 `time.After` 或 `ticker`；检查 `killed()` 退出
- Snapshot 时需原子替换状态 + 触发 `rf.persister.SaveStateAndSnapshot`

---
## 建议实现顺序
1. Lab1: 完成 MapReduce Master/Worker 最小可用路径 + 超时重试
2. Raft 2A -> 2B -> 2C (独立测试全部通过)
3. 基于稳定 Raft 构建 KV (幂等 + snapshot)
4. ShardMaster (配置变更与均衡算法)
5. ShardKV (迁移、配置推进、GC、snapshot 整合)
6. Porcupine 可选用于线性化验证

---
## 后续可添加的辅助文件
- `docs/RAFT_NOTES.md`: 记录算法细节 & 关键不变量
- `scripts/bench.sh`: 集成测试与性能统计

---
## 主要未实现 (集中 TODO 清单)
类别 | 任务 | 文件建议位置
-----|------|---------------
Raft 2A | 选举 & RequestVote | `raft/raft.go`
Raft 2B | AppendEntries & 日志复制 | `raft/raft.go`
Raft 2C | persist/readPersist & Snapshot | `raft/raft.go`, `raft/persister.go`
MapReduce | 任务调度/超时/阶段转换 | `mr/master.go`, `mr/worker.go`
KV | Op 去重 / 等待提交 / Snapshot | `kvraft/server.go`
ShardMaster | Join/Leave/Move/Query & 重新分配 | `shardmaster/server.go`
ShardKV | 配置拉取/迁移/GC | `shardkv/server.go`
通用 | 错误与日志封装 | 新建 `pkg/util` (可选)

---
## 代码风格/实践建议
- 禁止在 RPC Handler 中长时间持锁调用网络；先复制必要状态再解锁
- Channel 关闭时机：通常不关闭 `applyCh` 由上层控制；Kill 时仅设置标志
- 锁内不进行阻塞 IO / time.Sleep
- Snapshot/InstallSnapshot 需要与日志截断 carefully 同步

---
(自动生成完成)
