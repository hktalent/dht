package dht

import (
	_ "embed"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/multiformats/go-multiaddr"
	"github.com/pion/stun"
)

/*
https://nmap.org/nsedoc/scripts/stun-info.html
sudo nmap -sV -PN -sU -p 3478 --script stun-info <ip>
sudo nmap -sV -PN -sU -p 3478 --script stun-info numb.viagenie.ca
*/

type IStunList interface {
	GetStunList() []string
	GetDhtList() []string
	GetDhtMultiaddr() []multiaddr.Multiaddr
}
type StunList struct {
}

func (r StunList) GetDhtMma() []multiaddr.Multiaddr {
	var mma []multiaddr.Multiaddr
	a := r.GetDhtList()
	for _, s := range a {
		ma, err := multiaddr.NewMultiaddr(s)
		if err != nil {
			panic(err)
		}
		mma = append(mma, ma)
	}
	return mma
}
func (r StunList) GetDhtListArr() [][]string {
	a := r.GetDhtList()
	var aRst [][]string
	for _, x := range a {
		if -1 == strings.Index(x, "://") {
			aRst = append(aRst, []string{"udp://" + x + "/announce"})
		}
	}
	return aRst
}

//go:embed dhTrackers.txt
var bDhTrackers []byte

// https://newtrackon.com
// https://github.com/ngosang/trackerslist
// https://www.theunfolder.com/torrent-trackers-list/
func (r StunList) GetDhtListRawA() []string {
	return strings.Split(strings.TrimSpace(string(bDhTrackers)), "\n")
}

func (r StunList) GetDhtUdpLists() []string {
	a := r.GetDhtList()
	xR := []string{}
	for _, x := range a {
		if -1 < strings.Index(x, "udp://") {
			u, err := url.Parse(x)
			if err != nil {
				continue
			}
			xR = append(xR, u.Host)
		}
	}
	return xR
}

func (r StunList) GetDhtList() []string {
	a := r.GetDhtListRawA()
	for i, x := range a {
		if -1 == strings.Index(x, "://") {
			a[i] = "udp://" + x
		}
	}
	return a
}

//go:embed stunLists.txt
var stunTrackers []byte
var aStunLists []string

// 从data中查找element
func (r StunList) SliceIndex(element string, data []string) int {
	for k, v := range data {
		if element == v {
			return k
		}
	}
	return -1
}

// 这行必须放函数外面，全局存在，不然没有意义
var mu sync.Mutex

func rmIt(element string) {
	go func() {
		mu.Lock()
		defer mu.Unlock()
		if -1 == strings.Index(element, "google") {
			for k, v := range aStunLists {
				if element == v {
					if k+1 > len(aStunLists) {
						aStunLists = aStunLists[0:k]
					} else {
						aStunLists = append(aStunLists[0:k], aStunLists[1+k:len(aStunLists)]...)
					}
				}
			}
		}
	}()
}

// 获取stun服务器列表
func (r StunList) GetStunLists() []string {
	if 0 == len(aStunLists) {
		aStunLists = strings.Split(strings.TrimSpace(string(stunTrackers)), "\n")
	}
	return aStunLists
}

// 日志处理
func Log(a ...interface{}) {
	// fmt.Println(a...)
}

// 获取本机NAT的public ip和port
func (r StunList) GetSelfPublicIpPort() (string, int) {
	a := r.GetStunLists()

	// len(a)
	done := make(chan struct{}, len(a))
	doneClose := make(chan bool)
	ip := make(chan string, len(a))
	var s, szIp string
	var wg sync.WaitGroup
	var doOnce sync.Once
	for _, v1 := range a {
		xxx1 := v1
		done <- struct{}{}
		go func(v string) {
			wg.Add(1)
			defer func() {
				<-done
				wg.Done()
			}()
			select {
			case <-doneClose:
				return
			default:
				{
					c, err := stun.Dial("udp", v)
					if err != nil {
						rmIt(v)
						Log("1", err, v)
						return
					}
					defer c.Close()
					message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
					if err := c.Do(message, func(res stun.Event) {
						if res.Error != nil {
							rmIt(v)
							Log("2", res.Error, v)
							return
						}
						var xorAddr stun.XORMappedAddress
						if err := xorAddr.GetFrom(res.Message); err != nil {
							rmIt(v)
							Log("3", err, v)
							return
						}
						sss := fmt.Sprintf("%s:%d", xorAddr.IP, xorAddr.Port)
						ip <- sss
						doOnce.Do(func() {
							close(doneClose)
							close(done)
						})
					}); err != nil {
						rmIt(v)
						Log("4", err, v)

					}
				}
			}
		}(xxx1)
	}
	wg.Wait()

	var port int
	var err error
	// tick1 := time.Tick(time.Duration(time.Second * 10))
	select {
	// case <-tick1:
	// 	return szIp, port
	case s = <-ip:
		{
			// close(ip)
			// fmt.Println("close(doneClose) ...", len(done))
			a1 := strings.Split(s, ":")
			szIp = a1[0]
			if 1 < len(a1) {
				port, err = strconv.Atoi(a1[1])
				if err == nil {
				}
			}
			// close(done)
			return szIp, port
		}
	}
	// wg.Wait()
	return szIp, port
}

func (r StunList) GetSelfPublicIpPort1() (string, int) {
	a := r.GetStunLists()[0:4]
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	// len(a)
	var ip string
	// var wg sync.WaitGroup
	for _, v := range a {
		c, err := stun.Dial("udp", v)

		if err != nil {
			continue
		}
		defer c.Close()
		if err := c.Do(message, func(res stun.Event) {
			if res.Error != nil {
				return
			}
			var xorAddr stun.XORMappedAddress
			if err := xorAddr.GetFrom(res.Message); err != nil {
				return
			}
			sss := fmt.Sprintf("%s:%d", xorAddr.IP, xorAddr.Port)
			ip = sss
		}); err != nil {
			break
		}

	}
	var szIp string
	var port int
	var err error
	port = 0
	a1 := strings.Split(ip, ":")
	szIp = a1[0]
	if 1 < len(a1) {
		port, err = strconv.Atoi(a1[1])
		if err == nil {
		}
	}
	return szIp, port
}
