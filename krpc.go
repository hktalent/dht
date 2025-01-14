package dht

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

/* DHT 协议
通过 get_peers 找到节点
ping 发现坏死的、不活跃的节点，并移除出；判活心跳
find_node 用来查找某一个节点ID为Key的具体信息，信息里包括ip，port，ID
		  被用来查找给定 ID 的 node 的联系信息，请求包含 2 个参数，第一个参数是 id，包含了请求 node ID。第二个参数是 target，包含了请求者正在查找的 node ID。
		  当一个 node 接收到了 find_node 的 query，他应该给出对应的回复，回复中包含 2 个关键字 id 和 nodes，nodes 是字符串类型，包含了被请求 node 的路由表中最接近目标
		  node 的 K(8) 个最接近的 node 的联系信息
get_peers 用来查找某一个资源ID为Key的具体信息，信息里包含可提供下载该资源的ip:port列表
		  与种子文件的 infohash 有关。这时 q=get_peers。请求包含 2 个参数。第一个参数是 id，包含了请求 node 的 ID。第二个参数是 info_hash，它代表种子文件的 infohash
          如果被请求的 node 有对应 info_hash 的 peers，他将返回一个关键字 values，这是一个列表类型的字符串。每一个字符串包含了 CompactIP-address/portinfo 格式的 peers 信息。
		  如果被请求的 node 没有这个 infohash 的 peers，那么他将返回关键字 nodes，这个关键字包含了被请求 node 的路由表中离 info_hash 最近的 K 个 node，使用 Compactnodeinfo 格式回复。
		  在这两种情况下，关键字 token 都将被返回。之后的 annouce_peer 请求中必须包含 token。token 是一个短的二进制字符串
		  Infohash的16进制编码，共40字符
announce_peer 宣布控制查询节点的对等体正在端口上下载种子。场景，多机器同发布一个相同直的全局的hash，便于建立联系
*/
const (
	pingType         = "ping"
	findNodeType     = "find_node"
	getPeersType     = "get_peers"
	announcePeerType = "announce_peer"
)

const (
	generalError = 201 + iota
	serverError
	protocolError
	unknownError
)

// packet represents the information receive from udp.
type packet struct {
	data  []byte
	raddr *net.UDPAddr
}

// token represents the token when response getPeers request.
type token struct {
	data       string
	createTime time.Time
}

// tokenManager managers the tokens.
type tokenManager struct {
	*syncedMap
	expiredAfter time.Duration
	dht          *DHT
}

// newTokenManager returns a new tokenManager.
func newTokenManager(expiredAfter time.Duration, dht *DHT) *tokenManager {
	return &tokenManager{
		syncedMap:    newSyncedMap(),
		expiredAfter: expiredAfter,
		dht:          dht,
	}
}

// token returns a token. If it doesn't exist or is expired, it will add a
// new token.
func (tm *tokenManager) token(addr *net.UDPAddr) string {
	v, ok := tm.Get(addr.IP.String())
	tk, _ := v.(token)

	if !ok || time.Now().Sub(tk.createTime) > tm.expiredAfter {
		tk = token{
			data:       randomString(5),
			createTime: time.Now(),
		}

		tm.Set(addr.IP.String(), tk)
	}

	return tk.data
}

// clear removes expired tokens.
func (tm *tokenManager) clear() {
	for _ = range time.Tick(time.Minute * 3) {
		keys := make([]interface{}, 0, 100)

		for item := range tm.Iter() {
			if time.Now().Sub(item.val.(token).createTime) > tm.expiredAfter {
				keys = append(keys, item.key)
			}
		}

		tm.DeleteMulti(keys)
	}
}

// check returns whether the token is valid.
func (tm *tokenManager) check(addr *net.UDPAddr, tokenString string) bool {
	key := addr.IP.String()
	v, ok := tm.Get(key)
	tk, _ := v.(token)

	if ok {
		tm.Delete(key)
	}

	return ok && tokenString == tk.data
}

// makeQuery returns a query-formed data.
func makeQuery(t, q string, a map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"t": t,
		"y": "q",
		"q": q,
		"a": a,
	}
}

// makeResponse returns a response-formed data.
func makeResponse(t string, r map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"t": t,
		"y": "r",
		"r": r,
	}
}

// makeError returns a err-formed data.
func makeError(t string, errCode int, errMsg string) map[string]interface{} {
	return map[string]interface{}{
		"t": t,
		"y": "e",
		"e": []interface{}{errCode, errMsg},
	}
}

/*
send sends data to the udp.
发送异常就将ip加入黑名单了，这优点鲁棒
*/
func send(dht *DHT, addr *net.UDPAddr, data map[string]interface{}) error {
	dht.conn.SetWriteDeadline(time.Now().Add(time.Second * 15))

	_, err := dht.conn.WriteToUDP([]byte(Encode(data)), addr)
	if err != nil {
		dht.blackList.insert(addr.IP.String(), -1)
	}
	return err
}

// query represents the query data included queried node and query-formed data.
type query struct {
	node *node
	data map[string]interface{}
}

// transaction implements transaction.
type transaction struct {
	*query
	id       string
	response chan struct{}
}

// transactionManager represents the manager of transactions.
type transactionManager struct {
	*sync.RWMutex
	transactions *syncedMap
	index        *syncedMap
	cursor       uint64
	maxCursor    uint64
	queryChan    chan *query
	dht          *DHT
}

// newTransactionManager returns new transactionManager pointer.
func newTransactionManager(maxCursor uint64, dht *DHT) *transactionManager {
	return &transactionManager{
		RWMutex:      &sync.RWMutex{},
		transactions: newSyncedMap(),
		index:        newSyncedMap(),
		maxCursor:    maxCursor,
		queryChan:    make(chan *query, 1024),
		dht:          dht,
	}
}

// genTransID generates a transaction id and returns it.
func (tm *transactionManager) genTransID() string {
	tm.Lock()
	defer tm.Unlock()

	tm.cursor = (tm.cursor + 1) % tm.maxCursor
	return string(int2bytes(tm.cursor))
}

// newTransaction creates a new transaction.
func (tm *transactionManager) newTransaction(id string, q *query) *transaction {
	return &transaction{
		id:       id,
		query:    q,
		response: make(chan struct{}, tm.dht.Try+1),
	}
}

// genIndexKey generates an indexed key which consists of queryType and
// address.
func (tm *transactionManager) genIndexKey(queryType, address string) string {
	return strings.Join([]string{queryType, address}, ":")
}

// genIndexKeyByTrans generates an indexed key by a transaction.
func (tm *transactionManager) genIndexKeyByTrans(trans *transaction) string {
	return tm.genIndexKey(trans.data["q"].(string), trans.node.addr.String())
}

// insert adds a transaction to transactionManager.
func (tm *transactionManager) insert(trans *transaction) {
	tm.Lock()
	defer tm.Unlock()

	tm.transactions.Set(trans.id, trans)
	tm.index.Set(tm.genIndexKeyByTrans(trans), trans)
}

// delete removes a transaction from transactionManager.
func (tm *transactionManager) delete(transID string) {
	v, ok := tm.transactions.Get(transID)
	if !ok {
		return
	}

	tm.Lock()
	defer tm.Unlock()

	trans := v.(*transaction)
	tm.transactions.Delete(trans.id)
	tm.index.Delete(tm.genIndexKeyByTrans(trans))
}

// len returns how many transactions are requesting now.
func (tm *transactionManager) len() int {
	return tm.transactions.Len()
}

// transaction returns a transaction. keyType should be one of 0, 1 which
// represents transId and index each.
func (tm *transactionManager) transaction(
	key string, keyType int) *transaction {

	sm := tm.transactions
	if keyType == 1 {
		sm = tm.index
	}

	v, ok := sm.Get(key)
	if !ok {
		return nil
	}

	return v.(*transaction)
}

// getByTransID returns a transaction by transID.
func (tm *transactionManager) getByTransID(transID string) *transaction {
	return tm.transaction(transID, 0)
}

// getByIndex returns a transaction by indexed key.
func (tm *transactionManager) getByIndex(index string) *transaction {
	return tm.transaction(index, 1)
}

// transaction gets the proper transaction with whose id is transId and
// address is addr.
func (tm *transactionManager) filterOne(
	transID string, addr *net.UDPAddr) *transaction {

	trans := tm.getByTransID(transID)
	if trans == nil || trans.node.addr.String() != addr.String() {
		return nil
	}

	return trans
}

/*
query sends the query-formed data to udp and wait for the response.
When timeout, it will retry `try - 1` times, which means it will query
`try` times totally.
查询发生异常（失败）的节点就加入黑名单，并移出路由表
*/
func (tm *transactionManager) query(q *query, try int) {
	transID := q.data["t"].(string)
	trans := tm.newTransaction(transID, q)

	tm.insert(trans)
	defer tm.delete(trans.id)

	success := false
	for i := 0; i < try; i++ {
		if err := send(tm.dht, q.node.addr, q.data); err != nil {
			// log.Println(q.node.addr, err)
			break
		}

		select {
		case <-trans.response:
			success = true
			break
		case <-time.After(time.Second * 15):
			// case <-time.After(time.Second * 2):
		}
	}
	// 初始化时，还没有ready，就先不考虑黑名单问题，性能考虑，去掉条件：tm.dht.Ready &&
	if !success && q.node.id != nil {
		tm.dht.blackList.insert(q.node.addr.IP.String(), q.node.addr.Port)
		tm.dht.routingTable.RemoveByAddr(q.node.addr.String())
	}
}

// run starts to listen and consume the query chan.
func (tm *transactionManager) run() {
	var q *query
	// 限定query1024并发，不然会很卡
	xQ := make(chan struct{}, tm.dht.QueryWorkLimit*2)
	for {
		select {
		case q = <-tm.queryChan:
			// 这里必须异步： go，否则全部堵塞无法运行
			// go tm.query(q, tm.dht.Try)
			xQ <- struct{}{}
			go func(q1 *query) {
				defer func() {
					<-xQ
				}()
				tm.query(q1, tm.dht.Try)
			}(q)

		}
	}
}

// sendQuery send query-formed data to the chan.
func (tm *transactionManager) sendQuery(no *node, queryType string, a map[string]interface{}) {

	// If the target is self, then stop.
	if no.id != nil && no.id.RawString() == tm.dht.node.id.RawString() ||
		tm.getByIndex(tm.genIndexKey(queryType, no.addr.String())) != nil ||
		tm.dht.blackList.in(no.addr.IP.String(), no.addr.Port) {
		return
	}

	data := makeQuery(tm.genTransID(), queryType, a)
	tm.queryChan <- &query{
		node: no,
		data: data,
	}
}

// ping sends ping query to the chan.
func (tm *transactionManager) ping(no *node) {
	tm.sendQuery(no, pingType, map[string]interface{}{
		"id": tm.dht.id(no.id.RawString()),
	})
}

// findNode sends find_node query to the chan.
func (tm *transactionManager) findNode(no *node, target string) {
	tm.sendQuery(no, findNodeType, map[string]interface{}{
		"id":     tm.dht.id(target),
		"target": target,
	})
}

// getPeers sends get_peers query to the chan.
func (tm *transactionManager) getPeers(no *node, infoHash string) {
	tm.sendQuery(no, getPeersType, map[string]interface{}{
		"id":        tm.dht.id(infoHash),
		"info_hash": infoHash,
	})
}

/*
announcePeer sends announce_peer query to the chan.
implied_port 如果它存在且非零，则应忽略端口参数，而应使用UDP数据包的源端口作为对等体的端口,所以通常为1
*/
func (tm *transactionManager) announcePeer(
	no *node, infoHash string, impliedPort, port int, token string) {

	tm.sendQuery(no, announcePeerType, map[string]interface{}{
		"id":           tm.dht.id(no.id.RawString()),
		"info_hash":    infoHash,
		"implied_port": impliedPort,
		"port":         port,
		"token":        token,
	})
}

/*
ParseKey parses the key in dict data. `t` is type of the keyed value.
It's one of "int", "string", "map", "list".
*/
func ParseKey(data map[string]interface{}, key string, t string) error {
	val, ok := data[key]
	if !ok {
		return errors.New("lack of key")
	}

	switch t {
	case "string":
		_, ok = val.(string)
	case "int":
		_, ok = val.(int)
	case "map":
		_, ok = val.(map[string]interface{})
	case "list":
		_, ok = val.([]interface{})
	default:
		panic("invalid type")
	}

	if !ok {
		return errors.New("invalid key type")
	}

	return nil
}

// ParseKeys parses keys. It just wraps ParseKey.
func ParseKeys(data map[string]interface{}, pairs [][]string) error {
	for _, args := range pairs {
		key, t := args[0], args[1]
		if err := ParseKey(data, key, t); err != nil {
			return err
		}
	}
	return nil
}

// parseMessage parses the basic data received from udp.
// It returns a map value.
func parseMessage(data interface{}) (map[string]interface{}, error) {
	response, ok := data.(map[string]interface{})
	if !ok {
		return nil, errors.New("response is not dict")
	}

	if err := ParseKeys(
		response, [][]string{{"t", "string"}, {"y", "string"}}); err != nil {
		return nil, err
	}

	return response, nil
}

// handleRequest handles the requests received from udp.
func handleRequest(dht *DHT, addr *net.UDPAddr,
	response map[string]interface{}) (success bool) {

	t := response["t"].(string)

	if err := ParseKeys(
		response, [][]string{{"q", "string"}, {"a", "map"}}); err != nil {

		send(dht, addr, makeError(t, protocolError, err.Error()))
		return
	}

	q := response["q"].(string)
	a := response["a"].(map[string]interface{})

	if err := ParseKey(a, "id", "string"); err != nil {
		send(dht, addr, makeError(t, protocolError, err.Error()))
		return
	}

	id := a["id"].(string)

	if id == dht.node.id.RawString() {
		return
	}

	if len(id) != 20 {
		send(dht, addr, makeError(t, protocolError, "invalid id"))
		return
	}

	if no, ok := dht.routingTable.GetNodeByAddress(addr.String()); ok &&
		no.id.RawString() != id {

		dht.blackList.insert(addr.IP.String(), addr.Port)
		dht.routingTable.RemoveByAddr(addr.String())

		send(dht, addr, makeError(t, protocolError, "invalid id"))
		return
	}

	switch q {
	case pingType:
		send(dht, addr, makeResponse(t, map[string]interface{}{
			"id": dht.id(id),
		}))
	case findNodeType:
		if dht.IsStandardMode() {
			if err := ParseKey(a, "target", "string"); err != nil {
				send(dht, addr, makeError(t, protocolError, err.Error()))
				return
			}

			target := a["target"].(string)
			if len(target) != 20 {
				send(dht, addr, makeError(t, protocolError, "invalid target"))
				return
			}

			var nodes string
			targetID := newBitmapFromString(target)

			no, _ := dht.routingTable.GetNodeKBucktByID(targetID)
			if no != nil {
				nodes = no.CompactNodeInfo()
			} else {
				nodes = strings.Join(
					dht.routingTable.GetNeighborCompactInfos(targetID, dht.K),
					"",
				)
			}

			send(dht, addr, makeResponse(t, map[string]interface{}{
				"id":    dht.id(target),
				"nodes": nodes,
			}))
		}
	case getPeersType:
		if err := ParseKey(a, "info_hash", "string"); err != nil {
			send(dht, addr, makeError(t, protocolError, err.Error()))
			return
		}

		infoHash := a["info_hash"].(string)

		if len(infoHash) != 20 {
			send(dht, addr, makeError(t, protocolError, "invalid info_hash"))
			return
		}

		if dht.IsCrawlMode() {
			send(dht, addr, makeResponse(t, map[string]interface{}{
				"id":    dht.id(infoHash),
				"token": dht.tokenManager.token(addr),
				"nodes": "",
			}))
		} else if peers := dht.peersManager.GetPeers(
			infoHash, dht.K); len(peers) > 0 {

			values := make([]interface{}, len(peers))
			for i, p := range peers {
				values[i] = p.CompactIPPortInfo()
			}

			send(dht, addr, makeResponse(t, map[string]interface{}{
				"id":     dht.id(infoHash),
				"values": values,
				"token":  dht.tokenManager.token(addr),
			}))
		} else {
			send(dht, addr, makeResponse(t, map[string]interface{}{
				"id":    dht.id(infoHash),
				"token": dht.tokenManager.token(addr),
				"nodes": strings.Join(dht.routingTable.GetNeighborCompactInfos(
					newBitmapFromString(infoHash), dht.K), ""),
			}))
		}

		if dht.OnGetPeers != nil {
			dht.OnGetPeers(infoHash, addr.IP.String(), addr.Port)
		}
	case announcePeerType:
		if err := ParseKeys(a, [][]string{
			{"info_hash", "string"},
			{"port", "int"},
			{"token", "string"}}); err != nil {

			// 给个erro的响应
			send(dht, addr, makeError(t, protocolError, err.Error()))
			return
		}

		infoHash := a["info_hash"].(string)
		port := a["port"].(int)
		token := a["token"].(string)

		// 判断地址和token的一致性，不一致就返回
		// addr在管理器中就从管理器中删除
		if !dht.tokenManager.check(addr, token) {
			//			send(dht, addr, makeError(t, protocolError, "invalid token"))
			return
		}

		if impliedPort, ok := a["implied_port"]; ok &&
			impliedPort.(int) != 0 {
			port = addr.Port
		}

		// 伪装模式，接收DHT网络 数据包，监听功能
		if dht.IsStandardMode() {
			dht.peersManager.Insert(infoHash, newPeer(addr.IP, port, token))

			// 给个响应
			send(dht, addr, makeResponse(t, map[string]interface{}{
				"id": dht.id(id),
			}))
		}

		go dht.appendIps2DhtTracker(addr.String(), "")
		if dht.OnAnnouncePeer != nil {
			dht.OnAnnouncePeer(infoHash, addr.IP.String(), port)
		}
		// join 到新的匿名节点，加快自己的节点被发现的能力，加大自己节点的推广作用，2022-04-04 add
		// 缺点是，这样可能会被其他节点列入黑名单
		// dht.transactionManager.findNode(
		// 	&node{addr: addr},
		// 	dht.node.id.RawString(),
		// )
	default:
		//		send(dht, addr, makeError(t, protocolError, "invalid q"))
		return
	}

	no, _ := newNode(id, addr.Network(), addr.String())
	dht.routingTable.Insert(no)
	// 不管节点是什么，加Ta
	dht.Join2addr(addr.String())
	return true
}

/*
findOn puts nodes in the response to the routingTable, then if target is in
the nodes or all nodes are in the routingTable, it stops. Otherwise it
continues to findNode or getPeers.
*/
func findOn(dht *DHT, r map[string]interface{}, target *bitmap,
	queryType string) error {

	if err := ParseKey(r, "nodes", "string"); err != nil {
		return err
	}

	nodes := r["nodes"].(string)
	// 长度必须是26的倍数
	if len(nodes)%26 != 0 {
		return errors.New("the length of nodes should can be divided by 26")
	}

	hasNew, found := false, false
	for i := 0; i < len(nodes)/26; i++ {
		no, _ := newNodeFromCompactInfo(
			string(nodes[i*26:(i+1)*26]), dht.Network)

		if no.id.RawString() == target.RawString() {
			found = true
		}

		if dht.routingTable.Insert(no) {
			hasNew = true
		}
	}

	if found || !hasNew {
		return nil
	}

	targetID := target.RawString()
	for _, no := range dht.routingTable.GetNeighbors(target, dht.K) {
		switch queryType {
		case findNodeType:
			dht.transactionManager.findNode(no, targetID)
		case getPeersType:
			dht.transactionManager.getPeers(no, targetID)
		default:
			panic("invalid find type")
		}
	}
	return nil
}

/*
handleResponse handles responses received from udp.
黑名单中的ip再次有数据到来
   移出黑名单列表
   加入路由表
*/
func handleResponse(dht *DHT, addr *net.UDPAddr,
	response map[string]interface{}) (success bool) {

	t := response["t"].(string)

	trans := dht.transactionManager.filterOne(t, addr)
	if trans == nil {
		return
	}

	// inform transManager to delete the transaction.
	if err := ParseKey(response, "r", "map"); err != nil {
		return
	}

	q := trans.data["q"].(string)
	a := trans.data["a"].(map[string]interface{})
	r := response["r"].(map[string]interface{})

	if err := ParseKey(r, "id", "string"); err != nil {
		return
	}

	id := r["id"].(string)

	// If response's node id is not the same with the node id in the
	// transaction, raise error.
	if trans.node.id != nil && trans.node.id.RawString() != r["id"].(string) {
		dht.blackList.insert(addr.IP.String(), addr.Port)
		dht.routingTable.RemoveByAddr(addr.String())
		return
	}
	// 记录地址信息，便于将来使用，当然可能是临时的，也可能是长效的
	go dht.appendIps2DhtTracker(addr.String(), "")
	node, err := newNode(id, addr.Network(), addr.String())
	if err != nil {
		return
	}

	switch q {
	case pingType:
	case findNodeType:
		if trans.data["q"].(string) != findNodeType {
			return
		}

		target := trans.data["a"].(map[string]interface{})["target"].(string)
		if findOn(dht, r, newBitmapFromString(target), findNodeType) != nil {
			return
		}
	case getPeersType:
		if err := ParseKey(r, "token", "string"); err != nil {
			return
		}

		token := r["token"].(string)
		infoHash := a["info_hash"].(string)

		if err := ParseKey(r, "values", "list"); err == nil {
			values := r["values"].([]interface{})
			for _, v := range values {
				p, err := newPeerFromCompactIPPortInfo(v.(string), token)
				if err != nil {
					continue
				}
				dht.peersManager.Insert(infoHash, p)
				if dht.OnGetPeersResponse != nil {
					dht.OnGetPeersResponse(infoHash, p)
				}
			}
		} else if findOn(
			dht, r, newBitmapFromString(infoHash), getPeersType) != nil {
			return
		}
	case announcePeerType:
	default:
		return
	}

	// inform transManager to delete transaction.
	trans.response <- struct{}{}

	dht.blackList.delete(addr.IP.String(), addr.Port)
	dht.routingTable.Insert(node)

	return true
}

// handleError handles errors received from udp.
func handleError(dht *DHT, addr *net.UDPAddr,
	response map[string]interface{}) (success bool) {

	if err := ParseKey(response, "e", "list"); err != nil {
		return
	}

	if e := response["e"].([]interface{}); len(e) != 2 {
		return
	}

	if trans := dht.transactionManager.filterOne(
		response["t"].(string), addr); trans != nil {

		trans.response <- struct{}{}
	}

	return true
}

var handlers = map[string]func(*DHT, *net.UDPAddr, map[string]interface{}) bool{
	"q": handleRequest,
	"r": handleResponse,
	"e": handleError,
}

/*
handle handles packets received from udp.
检查黑名单ip，黑名单ip数据直接跳过
*/
func handle(dht *DHT, pkt packet) {
	if len(dht.workerTokens) == dht.PacketWorkerLimit {
		return
	}

	dht.workerTokens <- struct{}{}

	go func() {
		defer func() {
			<-dht.workerTokens
		}()

		if dht.blackList.in(pkt.raddr.IP.String(), pkt.raddr.Port) {
			return
		}

		data, err := Decode(pkt.data)
		if err != nil {
			return
		}

		response, err := parseMessage(data)
		if err != nil {
			return
		}

		if f, ok := handlers[response["y"].(string)]; ok {
			f(dht, pkt.raddr, response)
		}
	}()
}
