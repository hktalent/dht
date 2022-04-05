// Package dht implements the bittorrent dht protocol. For more information
// see http://www.bittorrent.org/beps/bep_0005.html.
package dht

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// StandardMode follows the standard protocol
	StandardMode = iota
	// CrawlMode for crawling the dht network.值为1
	CrawlMode
	bCloseRcdIps = true
)

var (
	// ErrNotReady is the error when DHT is not initialized.
	ErrNotReady = errors.New("dht is not ready")
	// ErrOnGetPeersResponseNotSet is the error that config
	// OnGetPeersResponseNotSet is not set when call dht.GetPeers.
	ErrOnGetPeersResponseNotSet = errors.New("OnGetPeersResponse is not set")
	ErrOnAnnouncePeerNotSet     = errors.New("OnAnnouncePeer is not set")
)

// Config represents the configure of dht.
type Config struct {
	// 本地节点id
	LocalNodeId string
	// in mainline dht, k = 8
	K int
	// for crawling mode, we put all nodes in one bucket, so KBucketSize may
	// not be K
	KBucketSize int
	// candidates are udp, udp4, udp6
	Network string
	// format is `ip:port`
	Address string
	// the prime nodes through which we can join in dht network
	PrimeNodes []string
	// the kbucket expired duration
	KBucketExpiredAfter time.Duration
	// the node expired duration
	NodeExpriedAfter time.Duration
	// how long it checks whether the bucket is expired
	CheckKBucketPeriod time.Duration
	// peer token expired duration
	TokenExpiredAfter time.Duration
	// the max transaction id
	MaxTransactionCursor uint64
	// how many nodes routing table can hold
	MaxNodes int
	// callback when got get_peers request
	OnGetPeers func(string, string, int)
	// callback when receive get_peers response
	OnGetPeersResponse func(string, *Peer)
	// callback when got announce_peer request
	OnAnnouncePeer func(string, string, int)
	// blcoked ips
	BlockedIPs []string
	// blacklist size
	BlackListMaxSize int
	// StandardMode or CrawlMode
	Mode int
	// the times it tries when send fails
	Try int
	// the size of packet need to be dealt with
	PacketJobLimit int
	// the size of packet handler
	PacketWorkerLimit int
	// the nodes num to be fresh in a kbucket
	RefreshNodeNum int
}

var (
	LocalNodeId = hex.EncodeToString([]byte("https://ee.51pwn.com"))[:20]
	g_nX        = 10
)

/*
NewStandardConfig returns a Config pointer with default values.
default:
	BlackListMaxSize:     65536
	MaxTransactionCursor:math.MaxUint32
	Address:    ":0"
	Network:     "udp4",
	K:           8,
	KBucketSize: 8,
	// 下面几个时间参数一般不要调整，是DHT协议的规范约束
	KBucketExpiredAfter、NodeExpriedAfter：15分钟
	CheckKBucketPeriod：30秒
	TokenExpiredAfter：10分钟

*/
func NewStandardConfig() *Config {
	return &Config{
		LocalNodeId: LocalNodeId,
		K:           8,
		KBucketSize: 8,
		Network:     "udp4",
		// fix: panic: listen udp4 :6881: bind: address already in use
		Address:    ":0",
		PrimeNodes: StunList{}.GetDhtUdpLists(),
		// 节点、kbucket有效期15分钟
		NodeExpriedAfter:    time.Duration(time.Minute * 15),
		KBucketExpiredAfter: time.Duration(time.Minute * 15),
		// kbucket检查 30秒
		CheckKBucketPeriod: time.Duration(time.Second * 30),
		// token有效期10分钟
		TokenExpiredAfter:    time.Duration(time.Minute * 10),
		MaxTransactionCursor: math.MaxUint32,
		MaxNodes:             5000 * g_nX,
		BlockedIPs:           make([]string, 0),
		BlackListMaxSize:     65536,
		Try:                  2,
		Mode:                 StandardMode,
		PacketJobLimit:       1024 * g_nX,
		PacketWorkerLimit:    256 * g_nX,
		RefreshNodeNum:       8 * g_nX,
	}
}

/*
NewCrawlConfig returns a config in crawling mode.
爬虫配置
1、节点和kbucket有效期为0
2、监测kbucket周期5秒
3、当前node为空节点
4、当前配置从 NewStandardConfig 获得模版后再进行修改的配置
*/
func NewCrawlConfig() *Config {
	config := NewStandardConfig()
	config.NodeExpriedAfter = 0
	config.KBucketExpiredAfter = 0
	config.CheckKBucketPeriod = time.Second * 5
	config.KBucketSize = math.MaxInt32
	// 空节点模式用于做爬虫专用
	config.Mode = CrawlMode
	config.RefreshNodeNum = 256

	return config
}

// DHT represents a DHT node.
type DHT struct {
	*Config
	node               *node
	conn               *net.UDPConn
	routingTable       *routingTable
	transactionManager *transactionManager
	peersManager       *peersManager
	tokenManager       *tokenManager
	blackList          *blackList
	Ready              bool
	packets            chan packet
	workerTokens       chan struct{}
}

/*
New returns a DHT pointer. If config is nil, then config will be set to
the default config.
注意：
1、创建了一个随机id的节点
workerTokens满了，数量等于 PacketWorkerLimit时，数据就丢弃
*/
func New(config *Config) *DHT {
	if config == nil {
		config = NewStandardConfig()
	}

	if "" == config.LocalNodeId {
		config.LocalNodeId = LocalNodeId
	}
	// node, err := newNode(config.LocalNodeId, config.Network, config.Address)
	// 每个节点id全球唯一，写死了要出问题
	node, err := newNode(randomString(20), config.Network, config.Address)
	if err != nil {
		panic(err)
	}

	d := &DHT{
		Config:       config,
		node:         node,
		blackList:    newBlackList(config.BlackListMaxSize),
		packets:      make(chan packet, config.PacketJobLimit),
		workerTokens: make(chan struct{}, config.PacketWorkerLimit),
	}

	for _, ip := range config.BlockedIPs {
		d.blackList.insert(ip, -1)
	}

	go func() {
		for _, ip := range getLocalIPs() {
			d.blackList.insert(ip, -1)
		}

		ip, err := getRemoteIP()
		if err != nil {
			d.blackList.insert(ip, -1)
		}
	}()

	return d
}

// IsStandardMode returns whether mode is StandardMode.
func (dht *DHT) IsStandardMode() bool {
	return dht.Mode == StandardMode
}

// IsCrawlMode returns whether mode is CrawlMode.
func (dht *DHT) IsCrawlMode() bool {
	return dht.Mode == CrawlMode
}

/*
init initializes global varables.
1、本地监听udp
2、初始化路由表
3、初始化peers管理器
4、初始化token管理器
5、初始化KRPC transaction管理器，运行，运行等于在定义的间隔时间内不停的query
*/
func (dht *DHT) init() {
	// 下面的注释打开后，内存开销过大
	nLen := len(dht.Config.PrimeNodes)
	if nLen > dht.Config.PacketWorkerLimit {
		dht.Config.PacketWorkerLimit = nLen + 8
	}
	if nLen > dht.Config.PacketJobLimit {
		dht.Config.PacketJobLimit = nLen + 8
	}
	if nLen > dht.Config.BlackListMaxSize {
		dht.Config.BlackListMaxSize = nLen + 8
	}

	listener, err := net.ListenPacket(dht.Network, dht.Address)
	if err != nil {
		panic(err)
	}

	dht.conn = listener.(*net.UDPConn)
	dht.routingTable = newRoutingTable(dht.KBucketSize, dht)
	dht.peersManager = newPeersManager(dht)
	dht.tokenManager = newTokenManager(dht.TokenExpiredAfter, dht)
	dht.transactionManager = newTransactionManager(
		dht.MaxTransactionCursor, dht)

	go dht.transactionManager.run()
	go dht.tokenManager.clear()
	go dht.blackList.clear()
}

func getIps(domain string) {
	a := strings.Split(domain, ":")
	ips, _ := net.LookupIP(a[0])
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			fmt.Printf("%v:%s\n", ipv4, a[1])
			// fmt.Println(string(ipv4) + ":" + a[1])
		}
	}
}

func SliceIndex(element string, data []string) int {
	for k, v := range data {
		if element == v {
			return k
		}
	}
	return -1
}

// 记录更多DHT Tracker ip:port 为下一版本提供更多加速的动力
func (dht *DHT) appendIps2DhtTracker(s string, fileName string) {
	if bCloseRcdIps {
		return
	}
	var n = -1
	if "" == fileName {
		fileName = "/ips.txt"
		n = SliceIndex(s, dht.Config.PrimeNodes)
	}
	dirname, err := os.UserHomeDir()
	if err != nil {
		return
	}
	// n = SliceIndex(s, dht.Config.PrimeNodes)
	// fmt.Println(n, " ", s)
	if -1 == n || fileName != "/ips.txt" {
		szNmae := dirname + fileName
		// fmt.Println("appendIps2DhtTracker ", szNmae, " start ")
		dht.Config.PrimeNodes = append(dht.Config.PrimeNodes, s)
		file, err := os.OpenFile(szNmae, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			// fmt.Println(err)
			return
		}
		defer file.Close()
		file.Write([]byte(s + "\n"))
		// fmt.Println("appendIps2DhtTracker ", szNmae, " ", s, n)
	}
}

/*
不断加入相邻、活跃、有效节点（加入DHT）
join makes current node join the dht network.
*/
func (dht *DHT) join() {
	wg := &sync.WaitGroup{}
	// 限制 4096 个并发
	ch := make(chan struct{}, dht.Config.PacketJobLimit)
	// fmt.Println(len(dht.PrimeNodes))
	// s1 := strconv.Itoa(len(dht.PrimeNodes))
	for _, addr := range dht.PrimeNodes {
		wg.Add(1)
		ch <- struct{}{}
		go func(addr string) {
			defer func() {
				wg.Done()
				<-ch

			}()
			raddr, err := net.ResolveUDPAddr(dht.Network, addr)
			// if err != nil {
			// 	// fmt.Println("error: ", addr, err)
			// 	return
			// }
			if err == nil {
				// go dht.appendIps2DhtTracker(raddr.String(), "/chinaOk.txt")
				// if ok, _ := regexp.Match(`^[0-9\.:]+$`, []byte(addr)); ok {
				// fmt.Println(addr)
				// } else {
				// 	getIps(addr)
				// }
				// NOTE: Temporary node has NOT node id.
				dht.transactionManager.findNode(
					&node{addr: raddr},
					dht.node.id.RawString(),
				)
			}
		}(addr)
	}
	wg.Wait()
}

/*
always from listen receives message from udp.
*/
func (dht *DHT) listen() {
	go func() {
		buff := make([]byte, 8192)
		for {
			n, raddr, err := dht.conn.ReadFromUDP(buff)
			if err == nil {
				dht.packets <- packet{buff[:n], raddr}
			}
		}
	}()
}

// id returns a id near to target if target is not null, otherwise it returns
// the dht's node id.
func (dht *DHT) id(target string) string {
	if dht.IsStandardMode() || target == "" {
		return dht.node.id.RawString()
	}
	return target[:15] + dht.node.id.RawString()[15:]
}

/*
1、通过infoHash 通知相邻节点，我在下载、关注infoHash的种子文件
*/
func (dht *DHT) AnnouncePeer(infoHash string) error {
	if !dht.Ready {
		return ErrNotReady
	}
	if dht.OnAnnouncePeer == nil {
		return ErrOnAnnouncePeerNotSet
	}
	if len(infoHash) == 40 {
		data, err := hex.DecodeString(infoHash)
		if err != nil {
			return err
		}
		infoHash = string(data)
	}
	// 相邻节点
	neighbors := dht.routingTable.GetNeighbors(
		newBitmapFromString(infoHash), dht.routingTable.Len())

	// no.id.RawString()
	for _, no := range neighbors {
		dht.transactionManager.announcePeer(no, infoHash, 1, no.addr.Port, dht.tokenManager.token(no.addr))
	}

	return nil
}

/*
GetPeers returns peers who have announced having infoHash.
GetPeers 向相邻节点发起匿名 infohash查询
注意：
   1、这种查询使用时需要间隔时间不停查询，直到有结果
   2、这里只是向当前内存路由表中临近的节点发起一次 get_peers 查询，没有查到是不管的

*/
func (dht *DHT) GetPeers(infoHash string) error {
	if !dht.Ready {
		return ErrNotReady
	}

	if dht.OnGetPeersResponse == nil {
		return ErrOnGetPeersResponseNotSet
	}

	if len(infoHash) == 40 {
		data, err := hex.DecodeString(infoHash)
		if err != nil {
			return err
		}
		infoHash = string(data)
	}
	// 相邻节点
	neighbors := dht.routingTable.GetNeighbors(
		newBitmapFromString(infoHash), dht.routingTable.Len())

	for _, no := range neighbors {
		dht.transactionManager.getPeers(no, infoHash)
	}

	return nil
}

/*
Run starts the dht.
1、初始化，监听
2、并行异步不停息接收udp数据
3、并行异步不停加入临近、活跃节点，也就是加入DHT网络
4、路由表的时候，继续加入joinDHT网络
5、transaction管理表 为空（size==0）的时候，刷新路由表生命周期
*/
func (dht *DHT) Run() {
	dht.init()

	dht.listen()
	dht.join()

	dht.Ready = true

	var pkt packet
	tick := time.Tick(dht.CheckKBucketPeriod)

	for {
		select {
		case pkt = <-dht.packets:
			handle(dht, pkt)
		case <-tick:
			if dht.routingTable.Len() == 0 {
				dht.join()
			} else if dht.transactionManager.len() == 0 {
				go dht.routingTable.Fresh()
			}
		}
	}
}
