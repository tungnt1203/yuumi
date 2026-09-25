// Lệnh egressproxy chạy 2 proxy cho container sandbox (issue #78, xem
// package egress). Server tự chạy lệnh này trong container "yuumi-egress"
// khi SANDBOX=docker:
//
//   - -listen (mặc định :3128): proxy CONNECT, chỉ cho host trong -allow.
//
//   - -cred-listen (mặc định :3129): reverse proxy tới Anthropic API, gắn
//     credential thật (CLAUDE_CODE_OAUTH_TOKEN hoặc ANTHROPIC_API_KEY trong
//     env của lệnh này) thay cho credential giả của sandbox.
//
//     go run ./cmd/egressproxy -listen :3128 -cred-listen :3129
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
	listen := flag.String("listen", ":3128", "địa chỉ proxy CONNECT")
	credListen := flag.String("cred-listen", ":3129", "địa chỉ credential proxy tới Anthropic API")
	allow := flag.String("allow", strings.Join(egress.DefaultAllowedHosts, ","), "các host được CONNECT tới (cổng 443), cách nhau bởi dấu phẩy")
	flag.Parse()

	cred, err := egress.CredentialFromOSEnv()
	if err != nil {
		log.Fatal(err)
	}

	hosts := strings.Split(*allow, ",")
	log.Printf("egress proxy listening on %s, allow: %s", *listen, strings.Join(hosts, ", "))
	log.Printf("credential proxy listening on %s, credential: %s", *credListen, cred.Env)

	errs := make(chan error, 2)
	go func() { errs <- serve(*listen, egress.New(hosts)) }()
	go func() { errs <- serve(*credListen, egress.NewCredentialProxy(cred, log.Printf)) }()
	log.Fatal(<-errs)
}

func serve(addr string, h http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}
