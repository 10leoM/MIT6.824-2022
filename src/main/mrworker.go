package main

//
// start a worker process, which is implemented
// in ../mr/worker.go. typically there will be
// multiple worker processes, talking to one master.
//
// go run mrworker.go wc.so
//
// Please do not change this file.
//

import (
	"fmt"
	"log"
	"os"
	"plugin"

	"../mr"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: mrworker xxx.so\n")
		os.Exit(1)
	}

	mapf, reducef := loadPlugin(os.Args[1])

	mr.Worker(mapf, reducef)
}

// load the application Map and Reduce functions
// from a plugin file, e.g. ../mrapps/wc.so
// 从插件文件加载 Map 和 Reduce 函数
func loadPlugin(filename string) (func(string, string) []mr.KeyValue, func(string, []string) string) {
	// 返回值1：map 函数
	// 返回值2：reduce 函数
	// 打开插件文件

	p, err := plugin.Open(filename) // 打开插件文件
	if err != nil {
		log.Fatalf("cannot load plugin %v", filename)
	}
	xmapf, err := p.Lookup("Map") // 查找 Map 函数
	if err != nil {
		log.Fatalf("cannot find Map in %v", filename)
	}
	mapf := xmapf.(func(string, string) []mr.KeyValue)
	xreducef, err := p.Lookup("Reduce")
	if err != nil {
		log.Fatalf("cannot find Reduce in %v", filename)
	}
	reducef := xreducef.(func(string, []string) string)

	return mapf, reducef
}
