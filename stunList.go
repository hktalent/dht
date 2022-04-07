package dht

import (
	_ "embed"
	"fmt"
	"net/url"
	"strconv"
	"strings"

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
type StunList struct{}

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
			aRst = append(aRst, []string{"udp://" + x})
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

// 获取stun服务器列表
func (r StunList) GetStunLists() []string {
	if 0 == len(aStunLists) {
		aStunLists = strings.Split(strings.TrimSpace(string(stunTrackers)), "\n")
	}
	return aStunLists
}

// 获取本机NAT的public ip和port
func (r StunList) GetSelfPublicIpPort() (string, int) {
	a := r.GetStunLists()[0:1]

	// len(a)
	done := make(chan struct{}, 16)
	doneClose := make(chan bool)
	ip := make(chan string)
	// var wg sync.WaitGroup
	for _, v1 := range a {
		select {
		case <-doneClose:
			break
		default:
			{
				xxx1 := v1
				done <- struct{}{}
				// wg.Add(1)
				go func(v string) {
					defer func() {
						<-done
						// wg.Done()
					}()
					c, err := stun.Dial("udp", v)
					if err != nil {
						return
					}
					message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
					if err := c.Do(message, func(res stun.Event) {
						if res.Error != nil {
							return
						}
						var xorAddr stun.XORMappedAddress
						if err := xorAddr.GetFrom(res.Message); err != nil {
							return
						}
						sss := fmt.Sprintf("%s:%d", xorAddr.IP, xorAddr.Port)
						ip <- sss
					}); err != nil {

					}
				}(xxx1)
			}
		}

	}
	var s, szIp string
	var port int
	var err error
	select {
	case s = <-ip:
		{
			close(doneClose)
			fmt.Println("close(doneClose) ...", len(done))
			a1 := strings.Split(s, ":")
			szIp = a1[0]
			port, err = strconv.Atoi(a1[1])
			if err == nil {
			}
			close(done)
		}
	}
	// wg.Wait()

	return szIp, port
}
