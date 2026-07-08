package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"runtime"

	"video-enhance/internal/enhancer"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "HTTP listen address")
	openBrowser := flag.Bool("open", false, "open the app in the default browser")
	flag.Parse()

	staticFS, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}

	server := enhancer.NewServer(enhancer.Config{
		WorkDir:   "jobs",
		OutputDir: "outputs",
	}, http.FS(staticFS))

	url := "http://" + *addr
	log.Printf("Video Enhance is running at %s", url)
	if *openBrowser {
		_ = open(url)
	}

	if err := http.ListenAndServe(*addr, server.Router()); err != nil {
		log.Fatal(err)
	}
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
