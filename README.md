![](https://raw.githubusercontent.com/hktalent/dht/master/dht.svg)
<img width="830" alt="image" src="https://user-images.githubusercontent.com/18223385/161761145-87883c44-9fa6-49f0-b113-5b1f8c37964e.png">

## what's the new
- :white_check_mark: update all depend mod to new
- :white_check_mark: update to go 1.18
- :white_check_mark: and config.LocalNodeId
- :white_check_mark: 45396 DHT tracker server ips，now,fly in at high speed to DHT network
- :white_check_mark: Rich annotations
- :white_check_mark: Friendly UML diagram rendering
- :white_check_mark: china,please use VPN over GWF
- :white_check_mark: fix 启动时卡顿问题
- :white_check_mark: fix do one time bug,now to tick 30 Second to do it
- :white_check_mark: fix public ip changed, cleanAll blackIp to do join

## Introduction

DHT implements the bittorrent DHT protocol in Go. Now it includes:

- [BEP-3 (part)](http://www.bittorrent.org/beps/bep_0003.html)
- [BEP-5](http://www.bittorrent.org/beps/bep_0005.html)
- [BEP-9](http://www.bittorrent.org/beps/bep_0009.html)
- [BEP-10](http://www.bittorrent.org/beps/bep_0010.html)

It contains two modes, the standard mode and the crawling mode. The standard
mode follows the BEPs, and you can use it as a standard dht server. The crawling
mode aims to crawl as more metadata info as possiple. It doesn't follow the
standard BEPs protocol. With the crawling mode, you can build another [BTDigg](http://btdigg.org/).

[bthub.io](http://bthub.io) is a BT search engine based on the crawling mode.

## Installation
```bash

go get -u github.com/hktalent/dht@latest

```

## Example

Below is a simple spider. You can move [here](https://github.com/hktalent/dht/blob/master/sample)
to see more samples.

```bash
cd sample/spider
go build spider.go
./spider -resUrl="http://127.0.0.1:9200/dht_index/_doc/" -address=":0"
```

```go
import (
    "fmt"
    "github.com/hktalent/dht"
)

func main() {
    downloader := dht.NewWire(65535)
    go func() {
        // once we got the request result
        for resp := range downloader.Response() {
            fmt.Println(resp.InfoHash, resp.MetadataInfo)
        }
    }()
    go downloader.Run()

    config := dht.NewCrawlConfig()
    config.OnAnnouncePeer = func(infoHash, ip string, port int) {
        // request to download the metadata info
        downloader.Request([]byte(infoHash), ip, port)
    }
    d := dht.New(config)

    d.Run()
}
```

## Download

You can download the demo compiled binary file [here](https://github.com/hktalent/dht/tags).

## Note

- The default crawl mode configure costs about 300M RAM. Set **MaxNodes**
  and **BlackListMaxSize** to fit yourself.
- Now it cant't run in LAN because of NAT.

## TODO

- :white_check_mark: NAT Traversal.
- :white_check_mark: Implements the full BEP-3.
- :white_check_mark: Optimization.

## FAQ

#### Why it is slow compared to other spiders ?

Well, maybe there are several reasons.

- DHT aims to implements the standard BitTorrent DHT protocol, not born for crawling the DHT network.
- NAT Traversal issue. You run the crawler in a local network.
- It will block ip which looks like bad and a good ip may be mis-judged.

## License

MIT, read more [here](https://github.com/hktalent/dht/blob/master/LICENSE)
