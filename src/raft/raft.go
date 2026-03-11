package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"bytes"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"../labgob"
	"../labrpc"
)

// import "bytes"
// import "labgob"

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
// 定义 ApplyMsg 结构，用于向服务（或测试器）发送已提交的日志条目
type ApplyMsg struct {
	CommandValid bool        // 是否包含有效命令
	Command      interface{} // 客户端请求的具体命令
	CommandIndex int         // 该日志条目的索引

	// 以下字段用于快照（Lab 3）
	SnapshotValid bool   // 是否包含有效快照
	Snapshot      []byte // 快照数据
	SnapshotTerm  int    // 快照中包含的最后一个日志条目的任期
	SnapshotIndex int    // 快照中包含的最后一个日志条目的索引
}

// Lab2A
// 定义 Raft 服务器的状态
type ManchineState int

const (
	Follower ManchineState = iota
	Candidate
	Leader
)

// LogEntry 定义日志条目的结构
type LogEntry struct {
	Command interface{} // 客户端请求的具体命令
	Term    int         // 该日志条目被添加时的任期号
	Index   int         // 该日志条目的索引(为什么要记录索引？因为日志条目可能会被删除，索引可以帮助我们定位日志条目在日志中的位置)
}

// AppendEntriesArgs 定义 AppendEntries RPC 的参数结构
type AppendEntriesArgs struct {
	Term     int // 领导者的任期号
	LeaderId int // 领导者的 ID
	// Follow会检查日志一致性，要求 PrevLogIndex 和 PrevLogTerm 与接收者的日志匹配，不匹配则减小 nextIndex 重试
	PrevLogIndex int // 新日志条目紧随之前的索引
	PrevLogTerm  int // 新日志条目紧随之前的任期号

	Entries      []LogEntry // 需要被存储的日志条目（可能为空，代表心跳）
	LeaderCommit int        // 领导者的已提交日志的索引
}

type AppendEntriesReply struct {
	Term          int  // 接收者的当前任期号
	Success       bool // 是否成功追加日志条目
	ConflictIndex int  // 日志不一致时，接收者的日志中第一个与领导者不匹配的条目的索引（仅在 Success 为 false 时有效）, 用于快速定位不一致点
	ConflictTerm  int  // 日志不一致时，接收者的日志中第一个与领导者不匹配的条目的任期号（仅在 Success 为 false 时有效）, 用于快速定位不一致点
}

// RequestVoteArgs 定义 RequestVote RPC 的参数结构
type RequestVoteArgs struct {
	Term         int // 候选人的任期号
	CandidateId  int // 候选人的 ID
	LastLogIndex int // 候选人最后一个日志条目的索引
	LastLogTerm  int // 候选人最后一个日志条目的任期号
}

type RequestVoteReply struct {
	Term        int  // 投票者的当前任期号
	VoteGranted bool // 是否投票给候选人
}

// InstallSnapshotArgs 定义 InstallSnapshot RPC 的参数结构（Lab 3）
type InstallSnapshotArgs struct {
	Term              int    // 领导者的任期号
	LeaderId          int    // 领导者的 ID
	LastIncludedIndex int    // 快照中包含的最后一个日志条目的索引
	LastIncludedTerm  int    // 快照中包含的最后一个日志条目的任期号
	Offset            int    // 本次传输的快照数据在整个快照中的偏移量
	Data              []byte // 快照数据，从 LastIncludedIndex 开始的日志条目
	Done              bool   // 是否为最后一批快照数据
}

type InstallSnapshotReply struct {
	Term int // 接收者的当前任期号，用于更新领导者的任期号
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // 用于保护共享访问的互斥锁
	peers     []*labrpc.ClientEnd // RPC 端点列表，表示集群中的其他节点
	persister *Persister          // 对象，用于保存该节点的持久化状态
	me        int                 // 该节点在 peers[] 中的索引
	dead      int32               // 设置为 true 时，节点将被终止

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	// 2A: 节点状态机，需要currentTerm, votedFor, state

	state     ManchineState // 节点状态：跟随者、候选人、领导者
	applyCh   chan ApplyMsg // 用于发送已提交日志条目的通道，恢复lastApplied
	applyCond *sync.Cond    // 用于通知 applier goroutine 有新的日志可以应用了

	// 需要持久化存储的状态
	currentTerm int        // 当前任期号
	votedFor    int        // 当前任期内投票给的候选人 ID
	log         []LogEntry // 日志条目列表

	// 易失性状态
	// 只要 commitIndex > lastApplied，节点就必须通过 applyCh 将日志中的指令发送给测试框架（状态机），并递增 lastApplied
	commitIndex int // 已提交的最高日志条目的索引
	lastApplied int // 已应用到状态机的最高日志条目的索引

	// 领导者特有的易失性状态
	nextIndex  []int // 对每个服务器，发送给它的下一个日志条目的索引
	matchIndex []int // 对每个服务器，已知复制到该服务器的最高日志条目的索引

	// 选举定时器相关
	randElectionTimer *time.Timer // 随机选举超时器
	lastHeard         time.Time   // 上次收到心跳或投票请求的时间

	// 通道用于处理选举和心跳
	heartbeatCh   chan struct{} // 用于接收心跳信号的通道
	triggerSendCh chan bool     // Trigger sending AppendEntries immediately

	// 快照相关
	lastIncludedIndex int // 快照中包含的最后一个日志条目的索引
	lastIncludedTerm  int // 快照中包含的最后一个日志条目的任期
}

// 创建一个新的 Raft 服务器实例
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rand.Seed(time.Now().UnixNano() + int64(me))
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	// 初始化 Raft 服务器的状态
	rf.currentTerm = 0           // 初始任期号为 0
	rf.votedFor = -1             // -1 表示尚未投票
	rf.log = make([]LogEntry, 1) // 索引 0 保留为空
	rf.state = Follower          // 初始状态为跟随者
	rf.applyCh = applyCh         // 应用日志条目的通道
	rf.commitIndex = 0           // 初始提交索引为 0
	rf.lastApplied = 0           // 初始已应用索引为 0

	// 领导者特有的状态初始化
	rf.nextIndex = make([]int, len(rf.peers))
	for i := range rf.peers {
		rf.nextIndex[i] = 1
	}
	rf.matchIndex = make([]int, len(rf.peers))
	for i := range rf.peers {
		rf.matchIndex[i] = 0
	}

	// 初始化选举定时器相关状态
	rf.heartbeatCh = make(chan struct{}, 1)
	rf.triggerSendCh = make(chan bool, 1)

	// 设置随机选举超时时间
	timeout := 300 + rand.Intn(200)
	rf.randElectionTimer = time.NewTimer(time.Duration(timeout) * time.Millisecond)

	// 初始化 applyCond 条件变量
	rf.applyCond = sync.NewCond(&rf.mu)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// 启动 applier goroutine 处理日志应用
	go rf.applier()

	// 启动后台 goroutine 处理选举和心跳
	go rf.handleTimeout()

	return rf
}

// return currentTerm and whether this server
// believes it is the leader.
// 2A: 获取当前任期号和是否为领导者
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// Your code here (2A).
	rf.mu.Lock()
	term = rf.currentTerm
	isleader = (rf.state == Leader)
	rf.mu.Unlock()

	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)

	rf.persister.SaveRaftState(rf.getRaftStateBytes())
}

func (rf *Raft) getRaftStateBytes() []byte {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)

	// 需要持久化 5 个状态 (增加了快照的两个属性)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)

	return w.Bytes()
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }

	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	// 解码 currentTerm, votedFor, log
	var currentTerm int
	var votedFor int
	var log []LogEntry
	var lastIncludedIndex int
	var lastIncludedTerm int

	if d.Decode(&currentTerm) != nil || d.Decode(&votedFor) != nil || d.Decode(&log) != nil || d.Decode(&lastIncludedIndex) != nil || d.Decode(&lastIncludedTerm) != nil {

	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.log = log
		rf.lastIncludedIndex = lastIncludedIndex
		rf.lastIncludedTerm = lastIncludedTerm

		// 恢复状态时，更新 commitIndex 和 lastApplied
		// 防止重启后 commitIndex 为 0，导致重复应用旧日志
		rf.commitIndex = lastIncludedIndex
		rf.lastApplied = lastIncludedIndex
	}

}

// ============================ Helper函数 =========================================================

// 获取日志的最后一个条目的全局索引和任期号
func (rf *Raft) lastLogInfo() (int, int) {
	l := len(rf.log)
	return l - 1 + rf.lastIncludedIndex, rf.log[l-1].Term
}

// 转换为 Follower 状态
func (rf *Raft) BecomeFollower(term int) {
	rf.state = Follower
	if term > rf.currentTerm {
		rf.currentTerm = term
		rf.votedFor = -1
		rf.persist()
	}
	rf.resetElectionTimer()
}

// 重置选举定时器，用于 Follower 和 Candidate 状态
func (rf *Raft) resetElectionTimer() {
	// 停止和清空定时器，否则会导致定时器过期后立即触发
	if !rf.randElectionTimer.Stop() {
		select {
		case <-rf.randElectionTimer.C:
		default:
		}
	}

	// 300 到 500 毫秒的随机超时时间
	rf.randElectionTimer.Reset(time.Duration(300+rand.Intn(200)) * time.Millisecond)
	rf.lastHeard = time.Now()
}

// 超时，转换为Candidate状态
func (rf *Raft) Convert2Candidate() {
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.persist() // 必须持久化：修改了 currentTerm 和 votedFor
	rf.resetElectionTimer()
}

// 调用 Kill() 方法后，Raft 服务器应该立即停止所有 goroutine
// 避免内存泄漏和 CPU 占用问题
// 标记 Raft 服务器为已死亡
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
	rf.mu.Lock()
	rf.applyCond.Broadcast()
	rf.mu.Unlock()
}

// 检查 Raft 服务器是否已被杀死
func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// ============================ RPC处理函数 =========================================================

// example RequestVote RPC handler.
// 处理 RequestVote RPC 请求
// 返回值：是否投票给该候选人
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A).
	// 2A: 实现投票逻辑
	// 2A1: 检查任期号，更新状态
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 如果请求的任期号小于当前任期号，拒绝投票
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		return
	}

	// 如果请求的任期号大于当前任期号，更新当前任期号和投票状态
	if args.Term > rf.currentTerm {
		rf.BecomeFollower(args.Term)
	}

	// 当前任期号>=请求的任期号，且当前节点没有投票给任何人，或者投票给的是请求投票的候选人，投票给该候选人
	reply.Term = rf.currentTerm
	lastIndex, lastTerm := rf.lastLogInfo()
	// 2A: 检查日志一致性，决定是否投票
	upToDate := args.LastLogTerm > lastTerm ||
		(args.LastLogTerm == lastTerm && args.LastLogIndex >= lastIndex)

	// 检查是否已经投票
	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && upToDate {
		// 投票给该候选人
		rf.votedFor = args.CandidateId
		reply.VoteGranted = true
		rf.resetElectionTimer()
		// 持久化
		rf.persist()
		return
	}

	// 否则拒绝投票
	reply.VoteGranted = false
	return
}

// 发送 RequestVote RPC 请求到指定服务器
// 返回值：是否成功发送 RPC 请求
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	// Your code here (2A, 2B).
	// 检查任期号，更新状态，重置选举定时器
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// 检查心跳
	// DPrintf('A', "Raft %d: receive AppendEntries RPC from leader %d, term %d, commitIndex %d, prevLogIndex %d, prevLogTerm %d", rf.me, args.LeaderId, args.Term, args.LeaderCommit, args.PrevLogIndex, args.PrevLogTerm)

	// 不合法的 AppendEntries 请求
	// 1.任期小于自己，拒绝处理
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.Success = false
		reply.ConflictIndex = -1
		return
	}

	// 如果 leader 发来的 PrevLogIndex 已经被自己丢弃了，直接告诉 leader 从最新的位置开始发
	if args.PrevLogIndex < rf.lastIncludedIndex {
		reply.Success = false
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		reply.ConflictTerm = -1
		return
	}

	// 2. 检查 PrevLogIndex 是否存在（日志太短）
	if rf.realIndex(args.PrevLogIndex) >= len(rf.log) {
		reply.ConflictIndex = len(rf.log) + rf.lastIncludedIndex
		reply.ConflictTerm = -1
		reply.Success = false
		return
	}

	// 3. 检查 PrevLogTerm 是否匹配
	if rf.log[rf.realIndex(args.PrevLogIndex)].Term != args.PrevLogTerm {
		reply.ConflictTerm = rf.log[rf.realIndex(args.PrevLogIndex)].Term

		// 找到该 Term 的第一条日志的索引 (用于跳过整个 Term)
		for i := args.PrevLogIndex; i >= rf.lastIncludedIndex; i-- {
			if rf.log[rf.realIndex(i)].Term != reply.ConflictTerm {
				reply.ConflictIndex = i + 1
				break
			}
			// 边界情况：如果是索引0
			if i == rf.lastIncludedIndex {
				reply.ConflictIndex = rf.lastIncludedIndex
			}
		}

		reply.Success = false
		return
	}

	// 4. 成功的处理
	reply.Term = rf.currentTerm
	reply.Success = true
	reply.ConflictIndex = -1
	// 更新任期和状态
	if args.Term >= rf.currentTerm {
		rf.BecomeFollower(args.Term)
	} else {
		rf.resetElectionTimer()
	}

	// 更新日志
	logChanged := false // 标记日志是否发生变化
	// 找到第一个不匹配的点
	for i, entry := range args.Entries {
		rindex := rf.realIndex(args.PrevLogIndex + 1 + i)
		if rindex < len(rf.log) {
			if rf.log[rindex].Term != entry.Term {
				// 冲突：截断并追加
				rf.log = rf.log[:rindex]
				rf.log = append(rf.log, args.Entries[i:]...)
				logChanged = true
				break
			}
			// Term 匹配，跳过（保留原样）
		} else {
			// 超出范围：直接追加
			rf.log = append(rf.log, args.Entries[i:]...)
			logChanged = true
			break
		}
	}

	if logChanged {
		rf.persist()
	}

	if args.LeaderCommit > rf.commitIndex {
		// 1. 找到本次 RPC 能确认的最新日志索引
		lastNewEntryIndex := args.PrevLogIndex + len(args.Entries)

		// 2. 取 LeaderCommit 和 本地确认索引 的较小值
		newCommitIndex := min(args.LeaderCommit, lastNewEntryIndex)

		// 3. 必须保证 commitIndex 单调递增，防止被乱序旧 RPC 覆盖降低
		if newCommitIndex > rf.commitIndex {
			rf.commitIndex = newCommitIndex
			rf.applyCond.Broadcast()
		}
	}

	if args.PrevLogIndex == -1 && args.PrevLogTerm == -1 {
		return
	}
	DPrintf('B', "Raft %d: receive AppendEntries RPC from leader %d, term %d, LeaderCommitIndex %d, prevLogIndex %d, prevLogTerm %d", rf.me, args.LeaderId, args.Term, args.LeaderCommit, args.PrevLogIndex, args.PrevLogTerm)
	return
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	// Your code here (Lab 3).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	DPrintf('D', "Raft %d: receive InstallSnapshot RPC from leader %d, term %d, lastIncludedIndex %d, lastIncludedTerm %d, serverLastIncludedIndex %d, serverLastIncludedTerm %d", rf.me, args.LeaderId, args.Term, args.LastIncludedIndex, args.LastIncludedTerm, rf.lastIncludedIndex, rf.lastIncludedTerm)

	// 1. 检查任期号，更新状态
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		return
	}

	// 2. 更新任期和状态
	if args.Term > rf.currentTerm {
		rf.BecomeFollower(args.Term)
	} else {
		rf.resetElectionTimer()
	}

	// 3. 更新快照相关状态
	if args.LastIncludedIndex < rf.lastIncludedIndex {
		// 已经有更新的快照了，忽略这个过时的快照
		reply.Term = rf.currentTerm
		return
	}
	// 4. 截断日志，保留快照包含的日志条目
	realIdx := rf.realIndex(args.LastIncludedIndex)
	if realIdx >= 0 && realIdx < len(rf.log) && rf.log[realIdx].Term == args.LastIncludedTerm {
		// 如果刚好有匹配的日志，保留后续的
		rf.log = rf.log[realIdx:]
	} else {
		// 如果冲突或者日志不够长，完全丢弃旧日志，仅保留一个占位符
		rf.log = []LogEntry{{
			Term:  args.LastIncludedTerm,
			Index: args.LastIncludedIndex,
		}}
	}
	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludedTerm = args.LastIncludedTerm

	// 5. 更新 commitIndex
	if rf.commitIndex < args.LastIncludedIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	rf.persister.SaveStateAndSnapshot(rf.getRaftStateBytes(), args.Data)

	// 6. 回复客户端
	reply.Term = rf.currentTerm

	// 7. 将快照数据发送给状态机
	// go func() {
	// 	applyMsg := ApplyMsg{
	// 		CommandValid:  false,
	// 		SnapshotValid: true,
	// 		Snapshot:      args.Data,
	// 		SnapshotTerm:  args.LastIncludedTerm,
	// 		SnapshotIndex: args.LastIncludedIndex,
	// 	}
	// 	rf.applyCh <- applyMsg
	// }()
	rf.applyCond.Broadcast()

	DPrintf('D', "Raft %d: receive InstallSnapshot RPC from leader %d, term %d, lastIncludedIndex %d, lastIncludedTerm %d", rf.me, args.LeaderId, args.Term, args.LastIncludedIndex, args.LastIncludedTerm)
}

func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

// =================================

// 运行 Raft 节点的主循环, 处理超时
// 新建的Raft节点，一开始都是Follower状态，所以第一个先做超时处理
/*
Follower：等待心跳或投票请求；若超时则转 Candidate，重置任期并自投票。
Candidate：启动新选举（term++、自投票、向其他节点并发 RequestVote），统计票数；
		   若获多数转 Leader，否则超时重选；若收到更高 term 的 AppendEntries/RequestVote 则退回 Follower。
Leader：周期性（~100ms）向所有节点发送空 AppendEntries 心跳以维持地位，
	    同时更新 nextIndex/matchIndex，若发现更高 term 的 RPC 返回则退回 Follower。
*/
func (rf *Raft) handleTimeout() {
	// DPrintf('A', "Raft %d: start handleTimeout goroutine", rf.me)
	for !rf.killed() {
		select {
		case <-rf.randElectionTimer.C:
			rf.mu.Lock()
			switch rf.state {
			case Follower, Candidate:
				rf.Convert2Candidate()
				rf.mu.Unlock()
				go rf.startElection()
			case Leader:
				rf.mu.Unlock()
			}
		}
	}
}

func (rf *Raft) startElection() {
	// 开始选举
	// Your code here (2A, 2B).

	var votes = 1          // 自己投一票
	var muVotes sync.Mutex // 保护 votes 变量的互斥锁
	var wg sync.WaitGroup  // 用于等待所有 RPC 调用完成
	won := false

	if rf.killed() {
		return
	}

	rf.mu.Lock()
	if rf.state != Candidate {
		rf.mu.Unlock()
		return
	}

	// 准备 RequestVote RPC 参数
	// DPrintf('A', "Raft %d: start election for term %d", rf.me, rf.currentTerm)

	lastIndex, lastTerm := rf.lastLogInfo()
	term := rf.currentTerm
	candidateId := rf.me
	var RequestVoteArgs = RequestVoteArgs{
		Term:         term,
		CandidateId:  candidateId,
		LastLogIndex: lastIndex,
		LastLogTerm:  lastTerm,
	}
	rf.mu.Unlock()

	// 向所有其他节点发送 RequestVote RPC
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		rf.mu.Lock()
		if rf.state != Candidate || rf.currentTerm != term {
			rf.mu.Unlock()
			return
		}
		rf.mu.Unlock()
		wg.Add(1)
		go func(server int) {
			defer wg.Done()
			var reply RequestVoteReply
			ok := rf.sendRequestVote(server, &RequestVoteArgs, &reply) // 阻塞等待回复
			if ok {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				if reply.Term > term {
					// 发现更高任期，转为 Follower
					// DPrintf('A', "[startElection fail] Raft %d: discovers higher term %d from server %d, becomes Follower", candidateId, reply.Term, server)
					rf.BecomeFollower(reply.Term)
					return
				}

				// 仍然是 Candidate 状态且当前任期与选举任期一致，处理投票结果
				if rf.state != Candidate || rf.currentTerm != term {
					// DPrintf('A', "[startElection fail]Raft %d: no longer Candidate, ignore vote from server %d", candidateId, server)
					return
				}

				if reply.VoteGranted && reply.Term == term {
					muVotes.Lock()
					// DPrintf('A', "Raft %d: receive vote from server %d, term %d", candidateId, server, reply.Term)
					votes++
					muVotes.Unlock()
					if votes > len(rf.peers)/2 {
						won = true
						rf.state = Leader
						for idx := range rf.peers {
							rf.nextIndex[idx] = len(rf.log)
							rf.matchIndex[idx] = 0
						}
						DPrintf('A', "Raft %d: becomes Leader for term %d, Get %d votes, peers %d", rf.me, term, votes, len(rf.peers))
						go rf.sendHeartbeats()
					}
				}

			}
			return
		}(i)
	}
	wg.Wait()

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != Candidate || rf.currentTerm != term || won {
		return
	}
	// DPrintf('A', "Raft %d: election failed for term %d, Get %d votes, peers %d", rf.me, term, votes, len(rf.peers))
}

func (rf *Raft) triggerSend() {
	select {
	case rf.triggerSendCh <- true:
	default:
	}
}

func (rf *Raft) sendHeartbeats() {
	for !rf.killed() {
		rf.mu.Lock()
		if rf.state != Leader {
			rf.mu.Unlock()
			return
		}

		for i := range rf.peers {
			if i == rf.me {
				continue
			}

			// 1. 构造参数
			nextIndex := rf.nextIndex[i]
			// 防御性检查
			if nextIndex <= 0 {
				nextIndex = 1
			}
			// 2. 如果 nextIndex 小于等于 lastIncludedIndex，说明该 follower 的日志已经落后太多了，直接发送 InstallSnapshot RPC（Lab 3）
			if nextIndex <= rf.lastIncludedIndex {
				snapArgs := InstallSnapshotArgs{
					Term:              rf.currentTerm,
					LeaderId:          rf.me,
					LastIncludedIndex: rf.lastIncludedIndex,
					LastIncludedTerm:  rf.lastIncludedTerm,
					Data:              rf.persister.ReadSnapshot(),
					Done:              true,
					Offset:            0,
					// 这里简化处理，直接发送整个快照，不分块传输
					// 实际上可以根据快照大小和网络状况分块传输，增加 Offset 和 Done 字段来标识分块状态
					// 但对于 Lab 3 来说，直接发送整个快照更简单，也能满足要求
				}
				DPrintf('D', "Raft %d: send InstallSnapshot RPC to server %d, term %d, nextIndex %d, lastIncludedIndex %d, lastIncludedTerm %d", rf.me, i, snapArgs.Term, nextIndex, snapArgs.LastIncludedIndex, snapArgs.LastIncludedTerm)
				go rf.sendSnapshotHelper(i, snapArgs)
				continue
			}

			// 3. 正常发送 AppendEntries RPC，准备参数
			prevLogIndex := nextIndex - 1
			// 如果 prevLogIndex 超出了当前日志范围（这不应该发生，但为了安全）
			if rf.realIndex(prevLogIndex) >= len(rf.log) {
				prevLogIndex = len(rf.log) - 1 + rf.lastIncludedIndex
			}
			prevLogTerm := rf.log[rf.realIndex(prevLogIndex)].Term
			// 准备要发送的 Entries
			var entries []LogEntry
			if rf.realIndex(nextIndex) < len(rf.log) {
				entries = make([]LogEntry, len(rf.log)-rf.realIndex(nextIndex))
				copy(entries, rf.log[rf.realIndex(nextIndex):])
			}
			args := AppendEntriesArgs{
				Term:         rf.currentTerm,
				LeaderId:     rf.me,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: rf.commitIndex,
			}

			// 4. 异步发送 RPC，处理回复
			go rf.boardcastHelper(i, args)
		}
		rf.mu.Unlock()

		// 频率不要太快也不要太慢，100ms 是比较合理的
		select {
		case <-rf.triggerSendCh:
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// 启动对新日志条目的共识过程，追加日志到本地日志中，并向其他节点发送 AppendEntries RPC 以复制日志条目
// 返回值：日志条目的索引、当前任期、是否为领导者
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != Leader {
		isLeader = false
		return index, term, isLeader
	}

	// 追加日志条目到本地日志中
	term = rf.currentTerm
	index = len(rf.log) + rf.lastIncludedIndex // 全局索引
	rf.log = append(rf.log, LogEntry{
		Command: command,
		Term:    term,
		Index:   index,
	})
	rf.persist() // 持久化状态
	DPrintf('B', "Raft %d: receive Start command, index %d, currentTerm %d", rf.me, index, term)

	// 发送 AppendEntries RPC 复制日志条目到其他节点
	rf.matchIndex[rf.me] = index
	rf.nextIndex[rf.me] = index + 1
	rf.triggerSend() // 接收到Start指令后立即触发一次日志复制，避免依赖心跳超时

	return index, term, isLeader
}

func (rf *Raft) boardcastHelper(server int, args AppendEntriesArgs) {
	// DPrintf('B', "Raft %d: send AppendEntries RPC to server %d, term %d, log index %d", rf.me, server, args.Term, args.PrevLogIndex+1)
	var reply AppendEntriesReply
	if rf.sendAppendEntries(server, &args, &reply) {
		rf.mu.Lock()
		defer rf.mu.Unlock()

		// 检查状态是否过期
		if rf.state != Leader || rf.currentTerm != args.Term {
			return
		}
		if reply.Term > rf.currentTerm {
			// 这里发现更高任期，Leader 转为 Follower
			rf.BecomeFollower(reply.Term)
			return
		}
		// 如果 AppendEntries RPC 成功且不是心跳，更新 matchIndex 和 nextIndex
		if reply.Success {
			newMatch := args.PrevLogIndex + len(args.Entries)
			if newMatch > rf.matchIndex[server] {
				rf.matchIndex[server] = max(rf.matchIndex[server], newMatch)
				rf.nextIndex[server] = rf.matchIndex[server] + 1
				rf.tryCommit() // 直接在锁内调用 tryCommit
			}
		} else if !reply.Success {
			// 失败
			// 快速回退逻辑
			possibleNextIdx := 1
			if reply.ConflictTerm == -1 {
				possibleNextIdx = reply.ConflictIndex
			} else {
				found := false
				var lastIndexInTerm int
				for i := len(rf.log) - 1; i >= 0; i-- {
					if rf.log[i].Term == reply.ConflictTerm {
						found = true
						lastIndexInTerm = i + rf.lastIncludedIndex
						break
					}
				}
				if found {
					possibleNextIdx = lastIndexInTerm + 1
				} else {
					possibleNextIdx = reply.ConflictIndex
				}
			}

			// 过滤过期的乱序回退指令
			// 只有当算出来的索引比当前尝试发送的 nextIndex 更小，且大于已经确认的 matchIndex 时，才回退！
			if possibleNextIdx < rf.nextIndex[server] && possibleNextIdx > rf.matchIndex[server] {
				rf.nextIndex[server] = possibleNextIdx
				rf.triggerSend()
			}
		}
	}
}

// 检查是否有日志条目可以提交，如果有则更新 commitIndex
// 注意：只能提交当前任期的日志条目，不能提交之前任期的日志条目
// TODO：锁优化
func (rf *Raft) tryCommit() {
	if rf.state != Leader {
		return
	}

	// 复制 matchIndex 以便排序，避免修改原切片
	matchIndexes := make([]int, len(rf.matchIndex))
	copy(matchIndexes, rf.matchIndex)
	matchIndexes[rf.me] = rf.lastIncludedIndex + len(rf.log) - 1 // 加上 Leader 自己的
	sort.Ints(matchIndexes)

	// 获取中位数 (Majority Index)
	// 比如 5 个节点，排序后是 [1, 2, 5, 5, 5]，中位数是下标 2 (5/2=2)，即 5
	// 只要有半数以上节点存储了该日志，该日志就安全
	n := len(rf.peers)
	newCommitIndex := matchIndexes[n-(n/2+1)]

	if newCommitIndex > rf.commitIndex {
		// 只有当前任期的日志可以提交
		if rf.log[rf.realIndex(newCommitIndex)].Term == rf.currentTerm {
			rf.commitIndex = newCommitIndex
			rf.applyCond.Broadcast()
			DPrintf('B', "Raft %d: commit log index update %d, currentTerm %d", rf.me, rf.commitIndex, rf.currentTerm)
		}
	}
}

// 应用日志到状态机
func (rf *Raft) applier() {
	for !rf.killed() {
		rf.mu.Lock()
		// 检查是否有待恢复/应用的日志
		for rf.commitIndex <= rf.lastApplied {
			rf.applyCond.Wait() // 等待 commitIndex 被更新的信号
		}

		// 如果上层当前的应用进度落后于刚收到的快照进度，统一在此下发 Snapshot！
		if rf.lastApplied < rf.lastIncludedIndex {
			applyMsg := ApplyMsg{
				CommandValid:  false,
				SnapshotValid: true,
				Snapshot:      rf.persister.ReadSnapshot(),
				SnapshotTerm:  rf.lastIncludedTerm,
				SnapshotIndex: rf.lastIncludedIndex,
			}
			rf.mu.Unlock() // 获取好消息后脱离锁发送，防止阻塞死锁

			rf.applyCh <- applyMsg

			rf.mu.Lock()
			rf.lastApplied = max(rf.lastApplied, rf.lastIncludedIndex)
			rf.commitIndex = max(rf.commitIndex, rf.lastIncludedIndex)
			rf.mu.Unlock()
			continue // 完成快照处理后重新进入一轮判定
		}

		// 批量获取待恢复的日志条目
		first := rf.lastApplied + 1
		last := rf.commitIndex
		entries := make([]LogEntry, last-first+1)
		copy(entries, rf.log[rf.realIndex(first):rf.realIndex(last+1)])

		rf.mu.Unlock()

		// 在锁外发送，防止 applyCh 阻塞导致死锁
		for i, entry := range entries {
			rf.applyCh <- ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: first + i,
			}
		}

		// 更新进度
		rf.mu.Lock()
		rf.lastApplied = max(rf.lastApplied, last)
		rf.mu.Unlock()
		DPrintf('B', "Raft %d: apply log index %d, currentTerm %d", rf.me, last, rf.currentTerm)
	}
}

// -------------- 2D 快照相关代码 --------------
// 获取当前 Raft 日志的大小
func (rf *Raft) GetRaftStateSize() int {
	return rf.persister.RaftStateSize()
}

// 获取当前真实的日志索引, 使用索引的地方都需要调用这个函数转换
func (rf *Raft) realIndex(logIndex int) int {
	// 快照中包含的最后一个日志条目的索引是 LastIncludedIndex
	// 真实的日志索引 = 日志索引 - 快照中包含的最后一个日志条目的索引
	return logIndex - rf.lastIncludedIndex
}

// 主动压缩日志，删除已提交的日志条目
// index: 快照中包含的最后一个日志条目的索引
// snapshot: 快照数据，上层 K/V 服务器传递给 Raft 层
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查索引是否有效
	if index < rf.lastIncludedIndex || index > rf.commitIndex {
		return
	}

	// 1. 先通过当前的 lastIncludedIndex 计算相对索引
	realIdx := rf.realIndex(index)

	// 2. 提前保存当次截断点的 Term
	newLastIncludedTerm := rf.log[realIdx].Term

	// 3. 删除已提交的日志条目，保留最后一个日志条目作为快照的基础，以及之后的日志条目
	newLog := make([]LogEntry, 0)
	newLog = append(newLog, rf.log[realIdx:]...)
	rf.log = newLog

	// 4. 最后更新快照相关状态
	rf.lastIncludedIndex = index
	rf.lastIncludedTerm = newLastIncludedTerm

	// 保存 Raft 状态和快照
	rf.persister.SaveStateAndSnapshot(rf.getRaftStateBytes(), snapshot)
	DPrintf('D', "Raft %d: snapshot index %d, lastIncludedTerm %d, log0Index %d, log0Term %d", rf.me, index, rf.lastIncludedTerm, rf.log[0].Index, rf.log[0].Term)
}

// 发送 InstallSnapshot RPC 给指定的 follower
func (rf *Raft) sendSnapshotHelper(server int, args InstallSnapshotArgs) {
	var reply InstallSnapshotReply
	if rf.sendInstallSnapshot(server, &args, &reply) {
		rf.mu.Lock()
		defer rf.mu.Unlock()
		// 落后回复
		if rf.state != Leader || rf.currentTerm != args.Term {
			return
		}

		if reply.Term > rf.currentTerm {
			rf.BecomeFollower(reply.Term)
			return
		}

		// 成功发送快照后，更新该 follower 的 nextIndex 和 matchIndex
		if args.LastIncludedIndex > rf.matchIndex[server] {
			rf.matchIndex[server] = args.LastIncludedIndex
			rf.nextIndex[server] = args.LastIncludedIndex + 1
		}
	}
}
