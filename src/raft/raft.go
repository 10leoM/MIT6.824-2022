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
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

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
type ApplyMsg struct {
	CommandValid bool        // 是否包含有效命令
	Command      interface{} // 客户端请求的具体命令
	CommandIndex int         // 该日志条目的索引
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
	Index   int         // 该日志条目的索引
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
	voteCh      chan struct{} // 用于接收投票结果的通道，Follower代表发送投票，Candidate代表收到投票
	heartbeatCh chan struct{} // 用于接收心跳信号的通道
}

// 创建一个新的 Raft 服务器实例
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
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
		rf.matchIndex[i] = 1
	}

	// 初始化选举定时器相关状态
	rf.voteCh = make(chan struct{}, 1)
	rf.heartbeatCh = make(chan struct{}, 1)

	// 设置随机选举超时时间
	timeout := 400 + rand.Intn(200)
	rf.randElectionTimer = time.NewTimer(time.Duration(timeout) * time.Millisecond)
	rf.resetElectionTimer()

	// 启动后台 goroutine 处理选举和心跳
	go rf.handleTimeout()

	// 初始化 applyCond 条件变量
	rf.applyCond = sync.NewCond(&rf.mu)
	// 启动 applier goroutine 处理日志应用
	go rf.applier()

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

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
}

// ============================ Helper函数 =========================================================

// 获取日志的最后一个条目的索引和任期号
func (rf *Raft) lastLogInfo() (int, int) {
	lastIndex := len(rf.log) - 1
	// 如果日志为空，返回 -1
	if lastIndex < 0 {
		return -1, -1
	}
	lastTerm := rf.log[lastIndex].Term
	return lastIndex, lastTerm
}

// 转换为 Follower 状态
func (rf *Raft) BecomeFollower(term int) {
	rf.state = Follower
	if term > rf.currentTerm {
		rf.currentTerm = term
		rf.votedFor = -1
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

	// 400 到 600 毫秒的随机超时时间
	rf.randElectionTimer.Reset(time.Duration(400+rand.Intn(200)) * time.Millisecond)
	rf.lastHeard = time.Now()
}

// 超时，转换为Candidate状态
func (rf *Raft) Convert2Candidate() {
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.resetElectionTimer()
}

// 调用 Kill() 方法后，Raft 服务器应该立即停止所有 goroutine
// 避免内存泄漏和 CPU 占用问题
// 标记 Raft 服务器为已死亡
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
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
	// Your code here (2A, 2B).
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
		// 通知选举协程收到投票
		select {
		case rf.voteCh <- struct{}{}:
		default:
		}
		return
	}

	// 否则拒绝投票
	reply.VoteGranted = false
	return

	// 2A2: 检查日志一致性，决定是否投票

	// 2B: 实现任期更新和日志一致性检查
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

	//  心跳逻辑

	// 2. 检查日志一致性
	if args.PrevLogIndex >= len(rf.log) || (args.PrevLogIndex >= 0 && rf.log[args.PrevLogIndex].Term != args.PrevLogTerm) {
		reply.Term = rf.currentTerm
		reply.Success = false
		lastIndex, _ := rf.lastLogInfo()
		if args.PrevLogIndex >= lastIndex {
			reply.ConflictIndex = lastIndex + 1
		} else {
			reply.ConflictIndex = -1
		}
		return
	}

	reply.Term = rf.currentTerm
	reply.Success = true
	reply.ConflictIndex = -1
	// 更新任期和状态
	if args.Term >= rf.currentTerm || rf.state != Follower {
		rf.BecomeFollower(args.Term)
	}
	// 更新日志
	rf.resetElectionTimer()

	// 从args.PrevLogIndex开始插入
	if args.PrevLogIndex >= 0 {
		rf.log = append(rf.log[:args.PrevLogIndex+1], args.Entries...)
	} else {
		rf.log = append(rf.log, args.Entries...)
	}
	if args.LeaderCommit > rf.commitIndex {
		// 取 min 是为了防止 Follower 提交了 Leader 还没发过来的日志
		// 必须取 len(rf.log)-1，因为我们刚刚更新了 log
		rf.commitIndex = min(args.LeaderCommit, len(rf.log)-1)
		rf.applyCond.Broadcast()
	}

	if args.PrevLogIndex == -1 && args.PrevLogTerm == -1 {
		return
	}
	DPrintf('B', "Raft %d: receive AppendEntries RPC from leader %d, term %d, commitIndex %d, prevLogIndex %d, prevLogTerm %d", rf.me, args.LeaderId, args.Term, args.LeaderCommit, args.PrevLogIndex, args.PrevLogTerm)
	return

}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
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
	DPrintf('A', "Raft %d: start handleTimeout goroutine", rf.me)
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
				return
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
	DPrintf('A', "Raft %d: start election for term %d", rf.me, rf.currentTerm)

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
					DPrintf('A', "[startElection fail] Raft %d: discovers higher term %d from server %d, becomes Follower", candidateId, reply.Term, server)
					rf.BecomeFollower(reply.Term)
					return
				}

				// 仍然是 Candidate 状态，处理投票结果
				if rf.state != Candidate {
					DPrintf('A', "[startElection fail]Raft %d: no longer Candidate, ignore vote from server %d", candidateId, server)
					return
				}

				if reply.VoteGranted && reply.Term == term {
					muVotes.Lock()
					DPrintf('A', "Raft %d: receive vote from server %d, term %d", candidateId, server, reply.Term)
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
	DPrintf('A', "Raft %d: election failed for term %d, Get %d votes, peers %d", rf.me, term, votes, len(rf.peers))
}

func (rf *Raft) sendHeartbeats() {
	// 发送心跳
	// Your code here (2A, 2B).
	DPrintf(1, "Raft %d: start sendHeartbeats goroutine", rf.me)

	for {
		// 发送心跳逻辑
		time.Sleep(100 * time.Millisecond)
		if rf.killed() { // 如果在发送AppendEntries RPC过程中leader被kill了就直接结束
			return
		}
		rf.mu.Lock()
		if rf.state != Leader {
			rf.mu.Unlock()
			return
		}

		term := rf.currentTerm
		commit := rf.commitIndex
		rf.mu.Unlock()

		for i := range rf.peers {
			if i == rf.me {
				continue
			}
			args := AppendEntriesArgs{
				Term:         term,
				LeaderId:     rf.me,
				PrevLogIndex: -1,
				PrevLogTerm:  -1,
				Entries:      nil,
				LeaderCommit: commit,
			}
			go func(server int, args AppendEntriesArgs) {
				var reply AppendEntriesReply
				if rf.sendAppendEntries(server, &args, &reply) {
					rf.mu.Lock()
					defer rf.mu.Unlock()
					if reply.Term > term {
						// 这里发现更高任期，Leader 转为 Follower，重新开始超时选举（仅Leader关闭超时选举，所以这里需要重新开启）
						rf.BecomeFollower(reply.Term)
						go rf.handleTimeout()
					}
				}
			}(i, args)
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
	index = len(rf.log)
	rf.log = append(rf.log, LogEntry{
		Command: command,
		Term:    term,
		Index:   index,
	})
	rf.persist() // 持久化状态
	DPrintf('B', "Raft %d: receive Start command, index %d, term %d", rf.me, index, term)

	// 发送 AppendEntries RPC 复制日志条目到其他节点
	rf.matchIndex[rf.me] = index
	rf.nextIndex[rf.me] = index + 1
	go rf.boardcastAppendEntries()

	return index, term, isLeader
}

// 广播 AppendEntries RPC 复制日志条目到其他节点
func (rf *Raft) boardcastAppendEntries() {
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		rf.mu.Lock()
		prevLogIndex := rf.nextIndex[i] - 1
		args := AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  rf.log[prevLogIndex].Term,
			Entries:      rf.log[prevLogIndex+1:],
			LeaderCommit: rf.commitIndex,
		}
		rf.mu.Unlock()
		go rf.boardcastHelper(i, args)
	}
}

func (rf *Raft) boardcastHelper(server int, args AppendEntriesArgs) {
	DPrintf('B', "Raft %d: send AppendEntries RPC to server %d, term %d, log index %d", rf.me, server, args.Term, args.PrevLogIndex+1)
	var reply AppendEntriesReply
	if rf.sendAppendEntries(server, &args, &reply) {
		rf.mu.Lock()
		defer rf.mu.Unlock()
		if reply.Term > args.Term {
			// 这里发现更高任期，Leader 转为 Follower，重新开始超时选举（仅Leader关闭超时选举，所以这里需要重新开启）
			rf.BecomeFollower(reply.Term)
			go rf.handleTimeout()
			return
		}
		// 如果 AppendEntries RPC 成功，更新 matchIndex 和 nextIndex
		if reply.Success {
			rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
			rf.nextIndex[server] = rf.matchIndex[server] + 1
			// 如果成功，检查是否有日志条目可以提交
			go rf.tryCommit()
		} else {
			// 如果 AppendEntries RPC 失败，减少 nextIndex 并重试
			if reply.ConflictIndex > 0 {
				rf.nextIndex[server] = reply.ConflictIndex
			} else {
				rf.nextIndex[server] = max(1, rf.nextIndex[server]-1)
				// go rf.boardcastHelper(server, AppendEntriesArgs{
				// 	Term:         rf.currentTerm,
				// 	LeaderId:     rf.me,
				// 	PrevLogIndex: rf.nextIndex[server] - 1,
				// 	PrevLogTerm:  rf.log[rf.nextIndex[server]-1].Term,
				// 	Entries:      []LogEntry{rf.log[rf.nextIndex[server]-1]},
				// 	LeaderCommit: rf.commitIndex,
				// })
			}
		}
	}
}

// 检查是否有日志条目可以提交，如果有则更新 commitIndex
// 注意：只能提交当前任期的日志条目，不能提交之前任期的日志条目
// TODO：锁优化
func (rf *Raft) tryCommit() {
	// 从 commitIndex + 1 开始寻找可以 commit 的最大 N
	// 倒序寻找可能更快，只要找到一个满足条件的 N 即可
	rf.mu.Lock()
	N := len(rf.log) - 1
	commitIndex := rf.commitIndex
	currentTerm := rf.currentTerm
	num := len(rf.peers)
	rf.mu.Unlock()

	for ; N > commitIndex; N-- {
		// 只有当前任期的日志可以通过计数方式提交
		// 之前任期的日志只能随当前任期日志一起被间接提交 (Log Matching Property)
		rf.mu.Lock()

		if rf.log[N].Term != rf.currentTerm {
			rf.mu.Unlock()
			break
		}

		defer rf.mu.Unlock()

		count := 0
		for i := range rf.peers {
			if rf.matchIndex[i] >= N {
				count++
			}
		}

		if count > num/2 {
			rf.commitIndex = N
			rf.applyCond.Broadcast() // 通知 applier goroutine 有新的日志可以应用了
			DPrintf('B', "Raft %d: commit log index %d, term %d", rf.me, N, currentTerm)
			break // 找到最大的 N 后即可退出
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

		// 批量获取待恢复的日志条目
		first := rf.lastApplied + 1
		last := rf.commitIndex
		entries := make([]LogEntry, last-first+1)
		copy(entries, rf.log[first:last+1])

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
		DPrintf('B', "Raft %d: apply log index %d, term %d", rf.me, last, rf.currentTerm)
	}
}
