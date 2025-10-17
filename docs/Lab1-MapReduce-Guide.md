# 6.824 Lab1 MapReduce 实验总结与实现指南

--- 

## 必读文件与角色分工

- `mr/master.go`：Master 调度逻辑（任务分配、状态机、阶段切换、容错、Done 判定）
- `mr/worker.go`：Worker 主循环（请求任务、执行 Map/Reduce、上报完成）、IO 读写
- `mr/rpc.go`：RPC 协议（请求/回复结构体、任务类型常量、masterSock 路径）
- `main/mrmaster.go`、`main/mrworker.go`、`main/mrsequential.go`：进程入口（分布式/顺序）
- `mrapps/*.go`：应用层 Map/Reduce 函数（wc/indexer/mtiming/rtiming/crash/nocrash）

---

## 任务与阶段模型（Master）

- 任务类型：Map / Reduce（可用统一的 `Task` 表示）
- 任务状态：Idle / InProgress / Completed
- 阶段（Phase）：MapPhase -> ReducePhase -> AllDone
- 计数器：`nMap` / `ncompletedMap`，`nReduce` / `ncompletedReduce`，用于阶段切换与 Done 判定
- 关键字段（建议）：
  - `TaskId`、`TaskType`、`TaskStatus`
  - `FileName`（Map 输入文件）/ `FileNames`（Reduce 要读取的中间文件列表）
  - `StartTime`（任务分配时刻，用于超时回收）

---

## 文件命名与数据格式（强约束，直接影响测试）

- 中间文件（Map 输出，当前目录）：`mr-<MapID>-<ReduceID>`，例如 `mr-2-0`
- 最终输出（Reduce 输出，当前目录）：`mr-out-<ReduceID>`，例如 `mr-out-0`
- 输出内容格式：每行 `key<空格>value\n`，无多余空格、无额外换行
- 写入方式：一律采用“临时文件 -> 原子改名”（tmp -> rename），避免半写文件（crash 测试依赖）
- 编码方式：中间文件用 JSON 行流（每个 KeyValue 一条 JSON），Reduce 端用 Decoder 连续 Decode 直到 EOF

---

## RPC 协议与交互流程

- 常量：`NoTask`、`MapTaskType`、`ReduceTaskType`
- 请求/回复（示例）：
  - RequestArgs{WorkerId}
  - RequestReply{TaskType, TaskId, FileName(用于Map), FileNames(用于Reduce), NReduce(用于Map)}
  - ReportArgs{WorkerId, TaskType, TaskId}
  - ReportReply{OK}
- 交互顺序（Worker 主循环）：请求任务 -> 执行 -> 上报完成 -> 重试/等待
- RPC 签名规范（net/rpc）：方法接收者为指针，参数是 `args *T`、`reply *U`，返回 `error`
- 字段首字母必须大写（Go RPC 反射要求）

---

## Worker 关键实现要点

- Map 任务：
  1) 读取输入文件，`kva := mapf(filename, content)`
  2) `reduceId = ihash(key) % NReduce` 分桶
  3) 每个分桶写当前目录临时文件（JSON 行流），完成后 `rename -> mr-MapID-ReduceID`
- Reduce 任务：
  1) 读取 `mr-MapID-ReduceID` 列表，逐文件 `json.NewDecoder(f).Decode(&kv)` 直到 `io.EOF`
  2) 聚合为 `map[key][]value` 或收集切片后排序
  3) 对 key 排序，逐 key 调用 `reducef(key, values)`，写临时文件，完成后 `rename -> mr-out-ReduceID`
- call() 封装：失败不要 `log.Fatal`，返回 false 给上层重试（健壮性更好，但最低限度不强制）

---

## Master 关键实现要点

- AssignTask（分配任务）：
  - MapPhase：优先分配 Idle 的 Map；若 `ncompletedMap == nMap` 切换到 ReducePhase
  - ReducePhase：分配 Idle 的 Reduce；若 `ncompletedReduce == nReduce` 置 AllDone
  - 分配时设置 `StartTime = time.Now()`
  - 无可分配（都在进行）时返回 `NoTask`
- ReportTaskDone（上报完成）：
  - 边界检查后，仅当任务当前状态不是 Completed 才置 Completed + 计数 + 清空 StartTime
  - `ncompletedMap == nMap` -> ReducePhase；`ncompletedReduce == nReduce` -> AllDone
- Done：返回 `phase == AllDone`（建议加锁读取）
- 容错（超时回收）：
  - 后台 goroutine 每 ~500ms 扫描当前阶段任务
  - 仅回收 `TaskStatus == InProgress` 且 `now - StartTime > 10s` 的任务 -> 置回 Idle、清空 StartTime
  - 注意用索引修改切片元素，不能用 `for _, t := range tasks`（值拷贝）

---

## Crash/并行性场景下的一致性保障

- 幂等写：所有输出通过 tmp -> rename 写最终文件
  - 多个 Worker 同时执行同一任务，最终文件由最后一次 rename 覆盖，不会出现半写
- 上报幂等：Master 在 Report 里仅在“第一次”将任务置 Completed 并计数，重复上报被忽略
- 只回收 InProgress：避免 Completed 被错误回收导致重复执行
- Reduce 输出稳定性：按 key 排序后再写，顺序不敏感，满足 test 脚本的 `sort` 对比

---

## 测试脚本（`src/main/test-mr.sh`）要求与对齐点

- 构建插件：`go build -buildmode=plugin XXX.go`（已在脚本中完成）
- 运行目录：脚本在 `mr-tmp/` 下运行，所有 `mr-*` 输出必须写在当前目录
- socket 路径：`/var/tmp/824-mr-<uid>`（与 `masterSock()` 一致）
- wc/indexer：合并 `mr-out*` 并排序再 cmp
- 并行性测试：统计输出行中 `times-` 或某些特征，验证并行程度
- crash 测试：不断启动/杀死 worker，验收输出与顺序版一致
- Worker 退出：作业完成后 worker 应退出（可在 AllDone 时 Master 返回 TaskExit，或 Worker 检测 `NoTask + Master exit`）

---

## 常见坑与修复建议

- JSON 解码：
  - 错误：`decoder.More()` 仅适用于数组上下文
  - 正确：循环 `Decode(&kv)`，直到 `io.EOF`
- 临时文件目录：必须在当前目录创建 tmp（`os.CreateTemp(".", pattern)`），不要写到系统临时目录
- range 值拷贝：`for _, t := range tasks` 修改的是副本；需要 `for i := range tasks { t := &tasks[i]; ... }`
- 超时回收误杀：只回收 InProgress，且 `StartTime` 非零、超过阈值
- RPC 字段大小写：传输结构体字段必须导出（首字母大写）
- GOPATH / Globs：
  - Lab 用 GOPATH 风格，`test-mr.sh` 已处理构建与运行
  - 命令行 `../pg*txt` 由 shell 展开；VS Code 调试需显式列出文件名

---

## 最小“通过路径”清单

1) Master：
   - [ ] `MakeMaster()` 初始化 Map/Reduce 任务与阶段
   - [ ] `AssignTask()` 分配 Idle 任务，设置 `StartTime`
   - [ ] `ReportTaskDone()` 幂等更新 + 计数 + 阶段切换
   - [ ] `Done()` 在 AllDone 时返回 true
   - [ ] 启动 `server()` + `startReclaimer()`（10s 超时回收）
2) Worker：
   - [ ] `Worker()` 主循环（请求 -> 执行 -> 上报）
   - [ ] `doMapTask()`/`handleMapTask()`：JSON 行流 + tmp->rename 写 `mr-MapID-ReduceID`
   - [ ] `doReduceTask()`/`handleReduceTask()`：JSON 解码到 EOF + 排序 + tmp->rename 写 `mr-out-ReduceID`
   - [ ] `call()` 失败不致命（可选增强）
3) RPC：
   - [ ] 对齐方法名：`Master.AssignTask`、`Master.ReportTaskDone`
   - [ ] 协议结构体字段首字母大写
4) 自检：
   - [ ] socket: `/var/tmp/824-mr-<uid>`
   - [ ] 所有输出生成在当前目录
   - [ ] 中间/最终文件名严格匹配测试脚本

---

## 运行与验证

- 快速运行脚本（推荐）：
  - `cd src/main && bash test-mr.sh`
- 手动验证（最小）：
  1) 启动 Master：`../mrmaster ../pg*txt &`
  2) 启动 Worker：`../mrworker ../../mrapps/wc.so &` 多开几个
  3) 等待 Master 退出，`sort mr-out* | cmp - mr-correct-wc.txt`

---

## 可选增强

- `TaskExit`：当 AllDone 时明确通知 Worker 退出
- 更细致的 backoff/retry 策略与错误日志
- Master 持久化任务状态（本 Lab 不要求）

---

## 参考代码片段（接口草案）

```go
// 任务类型常量
type TaskType int
const (
    NoTask TaskType = iota
    MapTaskType
    ReduceTaskType
)

// RPC 协议
type RequestArgs struct{ WorkerId int }
type RequestReply struct{
    TaskType TaskType
    TaskId   int
    FileName string   // Map 用
    FileNames []string // Reduce 用
    NReduce  int      // Map 用
}

type ReportArgs struct{ WorkerId int; TaskType TaskType; TaskId int }
type ReportReply struct{ OK bool }
```
---

## 结语

按本文完成后，应能稳定通过：wc、indexer、map/reduce 并行度、crash 测试。若仍未通过，优先检查：
- 文件命名/目录是否匹配脚本
- JSON 解码方式是否为“Decode 到 EOF”
- 任务超时回收是否只针对 InProgress
- 上报是否幂等
- 输出是否使用 tmp->rename
