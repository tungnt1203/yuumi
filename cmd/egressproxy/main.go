// Lệnh egressproxy chạy HTTP proxy giới hạn đường ra Internet của container
// sandbox (issue #78, bước 2, xem package egress). Server tự chạy lệnh này
// trong container "yuumi-egress" khi SANDBOX=docker; chạy tay để thử:
//
//	go run ./cmd/egressproxy -listen :3128
//	go run ./cmd/egressproxy -allow api.anthropic.com,proxy.golang.org
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/tungnt1203/yuumi/internal/egress"
)

func main() {
	listen := flag.String("listen", ":3128", "địa chỉ lắng nghe")
	allow := flag.String("allow", strings.Join(egress.DefaultAllowedHosts, ","), "các host được CONNECT tới (cổng 443), cách nhau bởi dấu phẩy")
	flag.Parse()

	hosts := strings.Split(*allow, ",")
	log.Printf("egress proxy listening on %s, allow: %s", *listen, strings.Join(hosts, ", "))

	server := &http.Server{
		Addr:              *listen,
		Handler:           egress.New(hosts),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}
