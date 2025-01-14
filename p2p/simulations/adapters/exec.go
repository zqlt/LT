















package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/pkg/reexec"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
)

func init() {
	
	
	reexec.Register("p2p-node", execP2PNode)
}



type ExecAdapter struct {
	
	
	BaseDir string

	nodes map[enode.ID]*ExecNode
}



func NewExecAdapter(baseDir string) *ExecAdapter {
	return &ExecAdapter{
		BaseDir: baseDir,
		nodes:   make(map[enode.ID]*ExecNode),
	}
}


func (e *ExecAdapter) Name() string {
	return "exec-adapter"
}


func (e *ExecAdapter) NewNode(config *NodeConfig) (Node, error) {
	if len(config.Lifecycles) == 0 {
		return nil, errors.New("node must have at least one service lifecycle")
	}
	for _, service := range config.Lifecycles {
		if _, exists := lifecycleConstructorFuncs[service]; !exists {
			return nil, fmt.Errorf("unknown node service %q", service)
		}
	}

	
	
	dir := filepath.Join(e.BaseDir, config.ID.String()[:12])
	if err := os.Mkdir(dir, 0755); err != nil {
		return nil, fmt.Errorf("error creating node directory: %s", err)
	}

	err := config.initDummyEnode()
	if err != nil {
		return nil, err
	}

	
	conf := &execNodeConfig{
		Stack: node.DefaultConfig,
		Node:  config,
	}
	if config.DataDir != "" {
		conf.Stack.DataDir = config.DataDir
	} else {
		conf.Stack.DataDir = filepath.Join(dir, "data")
	}

	
	conf.Stack.WSHost = "127.0.0.1"
	conf.Stack.WSPort = 0
	conf.Stack.WSOrigins = []string{"*"}
	conf.Stack.WSExposeAll = true
	conf.Stack.P2P.EnableMsgEvents = config.EnableMsgEvents
	conf.Stack.P2P.NoDiscovery = true
	conf.Stack.P2P.NAT = nil
	conf.Stack.NoUSB = true

	
	
	conf.Stack.P2P.ListenAddr = fmt.Sprintf(":%d", config.Port)

	node := &ExecNode{
		ID:      config.ID,
		Dir:     dir,
		Config:  conf,
		adapter: e,
	}
	node.newCmd = node.execCommand
	e.nodes[node.ID] = node
	return node, nil
}



type ExecNode struct {
	ID     enode.ID
	Dir    string
	Config *execNodeConfig
	Cmd    *exec.Cmd
	Info   *p2p.NodeInfo

	adapter *ExecAdapter
	client  *rpc.Client
	wsAddr  string
	newCmd  func() *exec.Cmd
}


func (n *ExecNode) Addr() []byte {
	if n.Info == nil {
		return nil
	}
	return []byte(n.Info.Enode)
}



func (n *ExecNode) Client() (*rpc.Client, error) {
	return n.client, nil
}



func (n *ExecNode) Start(snapshots map[string][]byte) (err error) {
	if n.Cmd != nil {
		return errors.New("already started")
	}
	defer func() {
		if err != nil {
			n.Stop()
		}
	}()

	
	confCopy := *n.Config
	confCopy.Snapshots = snapshots
	confCopy.PeerAddrs = make(map[string]string)
	for id, node := range n.adapter.nodes {
		confCopy.PeerAddrs[id.String()] = node.wsAddr
	}
	confData, err := json.Marshal(confCopy)
	if err != nil {
		return fmt.Errorf("error generating node config: %s", err)
	}

	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	statusURL, statusC := n.waitForStartupJSON(ctx)

	
	cmd := n.newCmd()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		envStatusURL+"="+statusURL,
		envNodeConfig+"="+string(confData),
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting node: %s", err)
	}
	n.Cmd = cmd

	
	status := <-statusC
	if status.Err != "" {
		return errors.New(status.Err)
	}
	client, err := rpc.DialWebsocket(ctx, status.WSEndpoint, "")
	if err != nil {
		return fmt.Errorf("can't connect to RPC server: %v", err)
	}

	
	n.client = client
	n.wsAddr = status.WSEndpoint
	n.Info = status.NodeInfo
	return nil
}


func (n *ExecNode) waitForStartupJSON(ctx context.Context) (string, chan nodeStartupJSON) {
	var (
		ch       = make(chan nodeStartupJSON, 1)
		quitOnce sync.Once
		srv      http.Server
	)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		ch <- nodeStartupJSON{Err: err.Error()}
		return "", ch
	}
	quit := func(status nodeStartupJSON) {
		quitOnce.Do(func() {
			l.Close()
			ch <- status
		})
	}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var status nodeStartupJSON
		if err := json.NewDecoder(r.Body).Decode(&status); err != nil {
			status.Err = fmt.Sprintf("can't decode startup report: %v", err)
		}
		quit(status)
	})
	
	
	go srv.Serve(l)
	go func() {
		<-ctx.Done()
		quit(nodeStartupJSON{Err: "didn't get startup report"})
	}()

	url := "http:
	return url, ch
}




func (n *ExecNode) execCommand() *exec.Cmd {
	return &exec.Cmd{
		Path: reexec.Self(),
		Args: []string{"p2p-node", strings.Join(n.Config.Node.Lifecycles, ","), n.ID.String()},
	}
}



func (n *ExecNode) Stop() error {
	if n.Cmd == nil {
		return nil
	}
	defer func() {
		n.Cmd = nil
	}()

	if n.client != nil {
		n.client.Close()
		n.client = nil
		n.wsAddr = ""
		n.Info = nil
	}

	if err := n.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return n.Cmd.Process.Kill()
	}
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- n.Cmd.Wait()
	}()
	select {
	case err := <-waitErr:
		return err
	case <-time.After(5 * time.Second):
		return n.Cmd.Process.Kill()
	}
}


func (n *ExecNode) NodeInfo() *p2p.NodeInfo {
	info := &p2p.NodeInfo{
		ID: n.ID.String(),
	}
	if n.client != nil {
		n.client.Call(&info, "admin_nodeInfo")
	}
	return info
}



func (n *ExecNode) ServeRPC(clientConn *websocket.Conn) error {
	conn, _, err := websocket.DefaultDialer.Dial(n.wsAddr, nil)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go wsCopy(&wg, conn, clientConn)
	go wsCopy(&wg, clientConn, conn)
	wg.Wait()
	conn.Close()
	return nil
}

func wsCopy(wg *sync.WaitGroup, src, dst *websocket.Conn) {
	defer wg.Done()
	for {
		msgType, r, err := src.NextReader()
		if err != nil {
			return
		}
		w, err := dst.NextWriter(msgType)
		if err != nil {
			return
		}
		if _, err = io.Copy(w, r); err != nil {
			return
		}
	}
}



func (n *ExecNode) Snapshots() (map[string][]byte, error) {
	if n.client == nil {
		return nil, errors.New("RPC not started")
	}
	var snapshots map[string][]byte
	return snapshots, n.client.Call(&snapshots, "simulation_snapshot")
}



type execNodeConfig struct {
	Stack     node.Config       `json:"stack"`
	Node      *NodeConfig       `json:"node"`
	Snapshots map[string][]byte `json:"snapshots,omitempty"`
	PeerAddrs map[string]string `json:"peer_addrs,omitempty"`
}




func execP2PNode() {
	glogger := log.NewGlogHandler(log.StreamHandler(os.Stderr, log.LogfmtFormat()))
	glogger.Verbosity(log.LvlInfo)
	log.Root().SetHandler(glogger)
	statusURL := os.Getenv(envStatusURL)
	if statusURL == "" {
		log.Crit("missing " + envStatusURL)
	}

	
	var status nodeStartupJSON
	stack, stackErr := startExecNodeStack()
	if stackErr != nil {
		status.Err = stackErr.Error()
	} else {
		status.WSEndpoint = "ws:
		status.NodeInfo = stack.Server().NodeInfo()
	}

	
	statusJSON, _ := json.Marshal(status)
	if _, err := http.Post(statusURL, "application/json", bytes.NewReader(statusJSON)); err != nil {
		log.Crit("Can't post startup info", "url", statusURL, "err", err)
	}
	if stackErr != nil {
		os.Exit(1)
	}

	
	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGTERM)
		defer signal.Stop(sigc)
		<-sigc
		log.Info("Received SIGTERM, shutting down...")
		stack.Close()
	}()
	stack.Wait() 
}

func startExecNodeStack() (*node.Node, error) {
	
	serviceNames := strings.Split(os.Args[1], ",")

	
	confEnv := os.Getenv(envNodeConfig)
	if confEnv == "" {
		return nil, fmt.Errorf("missing " + envNodeConfig)
	}
	var conf execNodeConfig
	if err := json.Unmarshal([]byte(confEnv), &conf); err != nil {
		return nil, fmt.Errorf("error decoding %s: %v", envNodeConfig, err)
	}

	
	nodeTcpConn, _ := net.ResolveTCPAddr("tcp", conf.Stack.P2P.ListenAddr)
	if nodeTcpConn.IP == nil {
		nodeTcpConn.IP = net.IPv4(127, 0, 0, 1)
	}
	conf.Node.initEnode(nodeTcpConn.IP, nodeTcpConn.Port, nodeTcpConn.Port)
	conf.Stack.P2P.PrivateKey = conf.Node.PrivateKey
	conf.Stack.Logger = log.New("node.id", conf.Node.ID.String())

	
	stack, err := node.New(&conf.Stack)
	if err != nil {
		return nil, fmt.Errorf("error creating node stack: %v", err)
	}

	
	
	services := make(map[string]node.Lifecycle, len(serviceNames))
	for _, name := range serviceNames {
		lifecycleFunc, exists := lifecycleConstructorFuncs[name]
		if !exists {
			return nil, fmt.Errorf("unknown node service %q", err)
		}
		ctx := &ServiceContext{
			RPCDialer: &wsRPCDialer{addrs: conf.PeerAddrs},
			Config:    conf.Node,
		}
		if conf.Snapshots != nil {
			ctx.Snapshot = conf.Snapshots[name]
		}
		service, err := lifecycleFunc(ctx, stack)
		if err != nil {
			return nil, err
		}
		services[name] = service
		stack.RegisterLifecycle(service)
	}

	
	stack.RegisterAPIs([]rpc.API{{
		Namespace: "simulation",
		Version:   "1.0",
		Service:   SnapshotAPI{services},
	}})

	if err = stack.Start(); err != nil {
		err = fmt.Errorf("error starting stack: %v", err)
	}
	return stack, err
}

const (
	envStatusURL  = "_P2P_STATUS_URL"
	envNodeConfig = "_P2P_NODE_CONFIG"
)


type nodeStartupJSON struct {
	Err        string
	WSEndpoint string
	NodeInfo   *p2p.NodeInfo
}


type SnapshotAPI struct {
	services map[string]node.Lifecycle
}

func (api SnapshotAPI) Snapshot() (map[string][]byte, error) {
	snapshots := make(map[string][]byte)
	for name, service := range api.services {
		if s, ok := service.(interface {
			Snapshot() ([]byte, error)
		}); ok {
			snap, err := s.Snapshot()
			if err != nil {
				return nil, err
			}
			snapshots[name] = snap
		}
	}
	return snapshots, nil
}

type wsRPCDialer struct {
	addrs map[string]string
}



func (w *wsRPCDialer) DialRPC(id enode.ID) (*rpc.Client, error) {
	addr, ok := w.addrs[id.String()]
	if !ok {
		return nil, fmt.Errorf("unknown node: %s", id)
	}
	return rpc.DialWebsocket(context.Background(), addr, "http:
}
