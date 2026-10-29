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
	"strings"
	"sync"
	"syscall"
	"time"

	webview "github.com/webview/webview_go"

	"github.com/rusik69/kui/internal/k8s"
	"github.com/rusik69/kui/internal/settings"
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
	rancherURL := flags.String("rancher-url", os.Getenv(settings.EnvURL), "Rancher management server URL (e.g. https://rancher.example.com); its clusters appear as \"rancher/<name>\" contexts")
	rancherTokenFlag := flags.String("rancher-token", "", "Rancher API bearer token (or set KUI_RANCHER_TOKEN; env var preferred to keep the token out of the process list)")
	rancherInsecure := flags.Bool("rancher-insecure", false, "skip TLS verification for the Rancher server (self-signed certificates)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	mgr := k8s.NewManager(*kubeconfig)
	rancherToken := settings.TokenFromFlagOrEnv(*rancherTokenFlag)
	flagConfiguredRancher := strings.TrimSpace(*rancherURL) != ""
	if flagConfiguredRancher {
		source, err := k8s.NewRancherSource(*rancherURL, rancherToken, *rancherInsecure)
		if err != nil {
			log.Printf("configure Rancher: %v", err)
			return 1
		}
		if strings.TrimSpace(rancherToken) == "" {
			log.Printf("rancher-url set without a token; set --rancher-token or KUI_RANCHER_TOKEN, or configure it in the UI")
		}
		mgr.SetRancher(source)
	}
	contextName, err := selectContext(mgr, *defaultContext)
	if err != nil {
		// No usable context yet (empty or unreadable kubeconfig, Rancher not
		// set up): still serve the UI shell so the Rancher settings dialog
		// and context switcher can bootstrap a setup from the browser.
		log.Printf("no usable context yet: %v — serving the shell anyway; configure a context or connect Rancher in the UI", err)
	}

	srv, err := web.New(mgr)
	if err != nil {
		log.Printf("init server: %v", err)
		return 1
	}
	defer srv.Close()
	srv.SetDebugImage(*debugImage)
	srv.SetDefaults(contextName, *defaultNamespace)
	srv.SetRancherToken(rancherToken)
	srv.InitRancher(flagConfiguredRancher)

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
	shutdownOnce := sync.Once{}
	gracefulShutdown := func() {
		shutdownOnce.Do(func() {
			log.Printf("stopping HTTP server on %s", ln.Addr())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				// Long-lived streams and requests stalled on an unreachable
				// cluster don't drain in time; close them so the process
				// can exit.
				log.Printf("shut down HTTP server: %v — closing remaining connections", err)
				server.Close()
			}
		})
	}
	defer gracefulShutdown()

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
		// Desktop mode: the webview's Run blocks on the platform event loop
		// and no signal handler was installed, so SIGINT/SIGTERM took the
		// default disposition — and in environments where the window never
		// opens (missing GTK/WebKitGTK), nothing could close the app at all.
		// Handle signals here: terminate the event loop so Run returns, then
		// fall through to the graceful server shutdown below. A watchdog
		// forces the process to exit if the loop refuses to stop.
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		stop = cancel
		w := webview.New(false)
		if w == nil {
			// webview creation failed (missing GTK 3/WebKitGTK on Linux, for
			// example). Fall back to headless serving instead of crashing on
			// a nil window with no way to close it.
			log.Printf("could not create a native window (missing GTK 3/WebKitGTK?); serving headless — open the URL in a browser and stop kui with Ctrl+C")
			fmt.Printf("kui listening on %s\n", url)
			<-ctx.Done()
		} else {
			windowClosed := make(chan struct{})
			go func() {
				<-ctx.Done()
				log.Printf("signal received; closing window and shutting down")
				// webview's Terminate must run on the platform's main thread
				// (on macOS, [NSApp stop:] from a background thread does not
				// stop the run loop); Dispatch gets it there.
				w.Dispatch(w.Terminate)
			}()
			go func() {
				// Watchdog: if the window has not closed 15s after a signal,
				// exit hard so the app can never linger. Normal close paths
				// cancel this by closing windowClosed (see below).
				<-ctx.Done()
				select {
				case <-windowClosed:
				case <-time.After(15 * time.Second):
					log.Printf("window did not close in time; exiting")
					server.Close()
					os.Exit(1)
				}
			}()
			openDesktopWindow(w, url)
			close(windowClosed)
		}
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
	gracefulShutdown()
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
	Terminate()
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
		// No current-context (e.g. a Rancher-only setup with no kubeconfig
		// defaults): fall back to the first available context, mirroring the
		// web UI's fallback for a missing context.
		if len(contexts) > 0 {
			return contexts[0].Name, nil
		}
		return "", errors.New("no contexts available; connect Rancher in the UI or fix the kubeconfig")
	}
	for _, ctx := range contexts {
		if ctx.Name == selected {
			return selected, nil
		}
	}
	return "", fmt.Errorf("kubeconfig context %q does not exist", selected)
}
