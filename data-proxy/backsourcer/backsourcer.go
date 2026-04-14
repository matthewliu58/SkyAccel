package backsourcer

import (
	"log/slog"
	"net"
	"sync"
	"time"

	"data-proxy/aggregator"
	"data-proxy/util"
)

const (
	workerCount   = 18
	taskQueueSize = 4096
	dialTimeout   = 3 * time.Second
	ioTimeout     = 5 * time.Second
)

type BackSourceTask struct {
	HopIP      [4]uint32
	Port       uint16
	UserID     uint32
	OriginAddr string //ip:port
	ReqData    []byte
}

type OriginProtocol interface {
	DoRequest(addr string, reqData []byte) ([]byte, error)
}

type BackSourcer struct {
	taskChan chan *BackSourceTask
	wg       sync.WaitGroup
	protocol OriginProtocol
}

var (
	BackSourcerMap = make(map[string]*BackSourcer)
)

func NewBackSourcer(protocol string, pre string, l *slog.Logger) *BackSourcer {

	l.Info("NewBackSourcer", slog.String("protocol", protocol), slog.String("pre", pre))

	switch protocol {
	case "udp":
		return NewBackSourcerWithProtocol(NewUDPProtocol(dialTimeout, ioTimeout), l)
	case "tcp":
		return NewBackSourcerWithProtocol(NewTCPProtocol(dialTimeout, ioTimeout), l)
	}
	return nil
}

func NewBackSourcerWithProtocol(p OriginProtocol, l *slog.Logger) *BackSourcer {
	bs := &BackSourcer{
		taskChan: make(chan *BackSourceTask, taskQueueSize),
		protocol: p,
	}
	bs.startWorkers(l)
	return bs
}

func (bs *BackSourcer) Submit(task *BackSourceTask) {
	bs.taskChan <- task
}

func (bs *BackSourcer) startWorkers(l *slog.Logger) {
	for i := 0; i < workerCount; i++ {
		bs.wg.Add(1)
		go func() {
			defer bs.wg.Done()
			for task := range bs.taskChan {
				bs.doOriginRequest(task, l)
			}
		}()
	}
}

func (bs *BackSourcer) doOriginRequest(task *BackSourceTask, l *slog.Logger) {
	if task == nil {
		return
	}

	l.Info("doOriginRequest", slog.String("originAddr", task.OriginAddr),
		slog.Any("UserID", task.UserID), slog.Any("port", task.Port))

	resp, err := bs.protocol.DoRequest(task.OriginAddr, task.ReqData)
	if err != nil || len(resp) == 0 {
		return
	}

	l.Info("doOriginRequest HopIP", slog.Any("HopIP", task.HopIP))

	var hops []net.IP
	for i := len(task.HopIP) - 1; i >= 0; i-- {
		if task.HopIP[i] == 0 {
			continue
		}
		hops = append(hops, util.Uint32ToIP(task.HopIP[i]))
	}

	if len(hops) > 1 {
		hops = hops[1:]
	}

	hopStrs := make([]string, 0, len(hops))
	routingKey := ""
	for _, hop := range hops {
		routingKey += "," + hop.String()
		hopStrs = append(hopStrs, hop.String())
	}

	l.Info("doOriginRequest response HopIP", slog.Any("HopIP", hopStrs))

	routingInfo := util.PathInfo{Hops: hopStrs}
	nextHop := hops[1]

	aggregator.GlobalAggResponse.AddToBatch(
		false,
		routingKey,
		0,
		routingInfo,
		nextHop,
		task.UserID,
		resp,
	)
}
