package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	webview "github.com/webview/webview_go"

	"github.com/rusik69/kui/internal/k8s"
	"github.com/rusik69/kui/internal/web"
)

func main() {
	os.Exit(run())
}

func run() int {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	kubeconfig := flags.String("kubeconfig", "", "path to kubeconfig (defaults to $KUBECONFIG or ~/.kube/config)")
	addr := flags.String("addr", "127.0.0.1:0", "listen address (default loopback; use an explicit external address to expose beyond this machine)")
	headless := flags.Bool("headless", false, "run as a plain HTTP server instead of opening a native window (for remote/SSH use)")
	debugImage := flags.String("debug-image", k8s.DebugPodShellImage, "container image for ephemeral debug shells")
	defaultContext := flags.String("context", "", "default kubeconfig context (defaults to current-context)")
	defaultNamespace := flags.String("namespace", "", "default namespace (defaults to the selected context's namespace or all namespaces)")
	accessLog := flags.Bool("access-log", false, "log HTTP requests (method, path, status, duration)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	mgr := k8s.NewManager(*kubeconfig)
	contextName, err := selectContext(mgr, *defaultContext)
	if err != nil {
		log.Printf("load kubeconfig: %v", err)
		return 1
	}

	srv, err := web.New(mgr)
	if err != nil {
		log.Printf("init server: %v", err)
		return 1
	}
	defer srv.Close()
	srv.SetDebugImage(*debugImage)
	srv.SetDefaults(contextName, *defaultNamespace)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Printf("listen: %v", err)
		return 1
	}

	handler := srv.Handler()
	if *accessLog {
		handler = web.WithAccessLog(handler, log.Default())
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("shut down HTTP server: %v", err)
		}
	}()

	url := fmt.Sprintf("http://%s", ln.Addr())
	log.Printf("starting HTTP server on %s", ln.Addr())
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(ln) }()

	var stop context.CancelFunc
	if *headless {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		fmt.Printf("kui listening on %s\n", url)
		stop = cancel
		select {
		case <-ctx.Done():
		case err := <-serveErr:
			if !errors.Is(err, http.ErrServerClosed) {
				log.Printf("HTTP server: %v", err)
				server.Close()
				return 1
			}
		}
	} else {
		openDesktopWindow(webview.New(false), url)
		select {
		case err := <-serveErr:
			if !errors.Is(err, http.ErrServerClosed) {
				log.Printf("HTTP server: %v", err)
				server.Close()
				return 1
			}
		default:
		}
	}
	if stop != nil {
		stop()
	}
	log.Printf("stopping HTTP server on %s", ln.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("shut down HTTP server: %v", err)
	}
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server: %v", err)
			return 1
		}
	default:
	}
	return 0
}

type desktopWindow interface {
	SetTitle(string)
	SetSize(int, int, webview.Hint)
	Navigate(string)
	Run()
	Destroy()
}

func openDesktopWindow(window desktopWindow, url string) {
	window.SetTitle("kui")
	window.SetSize(1280, 800, webview.HintNone)
	window.Navigate(url)
	window.Run()
	window.Destroy()
}

func selectContext(mgr *k8s.Manager, override string) (string, error) {
	contexts, err := mgr.ListContexts()
	if err != nil {
		return "", fmt.Errorf("read kubeconfig: %w", err)
	}
	current, err := mgr.DefaultContext()
	if err != nil {
		return "", fmt.Errorf("read kubeconfig current context: %w", err)
	}
	selected := current
	if override != "" {
		selected = override
	}
	if selected == "" {
		return "", errors.New("kubeconfig has no current-context; specify --context")
	}
	for _, ctx := range contexts {
		if ctx.Name == selected {
			return selected, nil
		}
	}
	return "", fmt.Errorf("kubeconfig context %q does not exist", selected)
}
