package kvraft

import (
	"crypto/rand"
	"math/big"

	"../labrpc"
)

type Clerk struct {
	servers []*labrpc.ClientEnd // 服务器列表，用于存储所有服务器的 RPC 客户端端
	// You will have to modify this struct.
	clientId     int64 // 客户端 ID，用于唯一标识客户端
	lastLeaderId int   // 上次联系的领导者 ID，用于优化 RPC 发送
	requestId    int64 // 请求 ID，用于唯一标识客户端的每个请求
}

// nrand 生成一个随机的 int64 类型的整数
// 可用于生成唯一的客户端 ID
func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	// You'll have to add code here.
	ck.clientId = nrand()
	ck.requestId = 0
	ck.lastLeaderId = 0
	return ck
}

// fetch the current value for a key.
// returns "" if the key does not exist.
// keeps trying forever in the face of all other errors.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
// Get 向服务器发送 Get 请求，用于获取键对应的值
// key: 键名
// 返回值：键对应的值
func (ck *Clerk) Get(key string) string {
	// You will have to modify this function.
	ck.requestId++

	// 发送 Get 请求
	for {
		args := GetArgs{
			Key:       key,
			ClientId:  ck.clientId,
			RequestId: ck.requestId,
		}
		reply := GetReply{}

		ok := ck.servers[ck.lastLeaderId].Call("KVServer.Get", &args, &reply)

		// 检查 RPC 是否成功以及业务逻辑是否执行
		if ok && (reply.Err == OK || reply.Err == ErrNoKey) {
			if reply.Err == ErrNoKey {
				return ""
			}
			return reply.Value
		}

		// 如果 RPC 失败，或者对方不是 Leader，尝试下一个服务器
		// 简单的轮询策略
		ck.lastLeaderId = (ck.lastLeaderId + 1) % len(ck.servers)
	}
	return ""
}

// shared by Put and Append.
//
// you can send an RPC with code like this:
// ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
//
// the types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. and reply must be passed as a pointer.
// PutAppend 向服务器发送 PutAppend 请求，用于更新键值对
// key: 键名
// value: 键对应的值
// op: 操作类型，"Put" 或 "Append"
// put: 如果键不存在，则创建；如果键已存在，则覆盖
// append: 如果键不存在，则创建；如果键已存在，则在原值后追加
func (ck *Clerk) PutAppend(key string, value string, op string) {
	// You will have to modify this function.
	ck.requestId++

	for {
		// 构造参数
		args := PutAppendArgs{
			Key:       key,
			Value:     value,
			Op:        op,
			ClientId:  ck.clientId,
			RequestId: ck.requestId,
		}
		reply := PutAppendReply{}

		ok := ck.servers[ck.lastLeaderId].Call("KVServer.PutAppend", &args, &reply)

		if ok && reply.Err == OK {
			return
		}

		// 失败重试：切换 Leader
		ck.lastLeaderId = (ck.lastLeaderId + 1) % len(ck.servers)
	}
}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}
