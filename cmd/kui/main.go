package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"

	webview "github.com/webview/webview_go"

	"github.com/rusik69/kui/internal/k8s"
	"github.com/rusik69/kui/internal/web"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig (defaults to $KUBECONFIG or ~/.kube/config)")
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on")
	headless := flag.Bool("headless", false, "run as a plain HTTP server instead of opening a native window (for remote/SSH use)")
	flag.Parse()

	mgr := k8s.NewManager(*kubeconfig)
	if _, err := mgr.DefaultContext(); err != nil {
		log.Fatalf("load kubeconfig: %v", err)
	}

	srv, err := web.New(mgr)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	go func() {
		log.Fatal(http.Serve(ln, srv.Handler()))
	}()

	url := fmt.Sprintf("http://%s", ln.Addr())
	if *headless {
		fmt.Printf("kui listening on %s\n", url)
		select {}
	}

	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle("kui")
	w.SetSize(1280, 800, webview.HintNone)
	w.Navigate(url)
	w.Run()
}
