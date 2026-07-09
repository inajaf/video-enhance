package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/pprof"
	"os/exec"
	"runtime"

	"video-enhance/internal/enhancer"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "HTTP listen address")
	openBrowser := flag.Bool("open", false, "open the app in the default browser")
	enablePprof := flag.Bool("pprof", false, "enable Go profiling endpoints under /debug/pprof/")
	flag.Parse()

	staticFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}

	server := enhancer.NewServer(enhancer.Config{
		WorkDir:   "jobs",
		OutputDir: "outputs",
	}, http.FS(staticFS))

	handler := server.Router()
	if *enablePprof {
		mux := http.NewServeMux()
		mux.Handle("/", handler)
		registerPprof(mux)
		handler = mux
	}

	url := "http://" + *addr
	log.Printf("Video Enhance is running at %s", url)
	if *enablePprof {
		log.Printf("Profiling endpoints are enabled at %s/debug/pprof/", url)
	}
	if *openBrowser {
		_ = open(url)
	}

	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}

func registerPprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}

func open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if cmd == nil {
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
