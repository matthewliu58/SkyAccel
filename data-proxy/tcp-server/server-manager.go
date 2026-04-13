package tcp_server

import (
	"github.com/gin-gonic/gin"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// 外部声明（由 main.go 提供）
var (
	logger       *slog.Logger
	accessLogger *slog.Logger
)

// 全局端口管理
var (
	portMap     = make(map[int]bool)
	portMutex   sync.RWMutex
	listenerMap = make(map[int]net.Listener)
	listenerMu  sync.RWMutex
)

// StartTCPServerWithMgr 创建 listener 并注册到管理器（不出错即可返回）
func StartTCPServerWithMgr(port int, pre string, access, l *slog.Logger) error {
	port_ := ":" + strconv.Itoa(port)
	listener, err := net.Listen("tcp", port_)
	if err != nil {
		return err
	}

	// 记录 listener
	listenerMu.Lock()
	listenerMap[port] = listener
	portMutex.Lock()
	portMap[port] = true
	portMutex.Unlock()
	listenerMu.Unlock()

	l.Info("tcp server started success", slog.String("pre", pre), slog.String("port", port_))
	return nil
}

// StartTCPServerRun 从 map 获取 listener，开始 accept 循环（阻塞）
func StartTCPServerRun(port int, pre string, access, l *slog.Logger) {
	listenerMu.RLock()
	listener, _ := listenerMap[port]
	listenerMu.RUnlock()

	for {
		conn, err := listener.Accept()
		if err != nil {
			l.Error("accept failed", slog.Any("err", err))
			continue
		}
		go handleConnection(conn, port, access, l)
	}
}

// StopTCPServer 停止指定端口的 TCP server
func StopTCPServer(port int) error {
	listenerMu.Lock()
	defer listenerMu.Unlock()

	listener, ok := listenerMap[port]
	if !ok {
		return nil // 已关闭
	}

	err := listener.Close()
	delete(listenerMap, port)
	portMutex.Lock()
	delete(portMap, port)
	portMutex.Unlock()

	return err
}

// TCPServerManager HTTP 接口管理 TCP server
func TCPServerManager(r *gin.Engine) {
	r.POST("/tcp/start", func(c *gin.Context) {
		portStr := c.Query("port")
		if portStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port is required"})
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
			return
		}

		// 检查是否已启动
		portMutex.RLock()
		exists := portMap[port]
		portMutex.RUnlock()

		if exists {
			c.JSON(http.StatusConflict, gin.H{"error": "port already started"})
			return
		}

		// 启动 TCP server（先创建 listener，失败则返回错误）
		err = StartTCPServerWithMgr(port, "tcp-manager", accessLogger, logger)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// 成功后再用 goroutine 运行
		go StartTCPServerRun(port, "tcp-manager", accessLogger, logger)

		c.JSON(http.StatusOK, gin.H{"message": "tcp server started", "port": port})
	})

	r.GET("/tcp/list", func(c *gin.Context) {
		portMutex.RLock()
		ports := make([]int, 0, len(portMap))
		for p := range portMap {
			ports = append(ports, p)
		}
		portMutex.RUnlock()
		c.JSON(http.StatusOK, gin.H{"ports": ports})
	})

	r.DELETE("/tcp/stop", func(c *gin.Context) {
		portStr := c.Query("port")
		if portStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "port is required"})
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid port"})
			return
		}

		err = StopTCPServer(port)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "tcp server stopped", "port": port})
	})
}
