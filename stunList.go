package dht

import (
	_ "embed"
	"net/url"
	"strings"

	"github.com/multiformats/go-multiaddr"
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
