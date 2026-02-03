package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/tar/s2s/pkg/crypto"
	"github.com/tar/s2s/pkg/relay"
)

func main() {
	listen := flag.String("listen", ":51820", "listen address")
	key := flag.String("key", "", "pre-shared key")
	flag.Parse()

	if *key == "" {
		log.Fatal("--key is required")
	}

	cipher, err := crypto.NewCipher(*key)
	if err != nil {
		log.Fatalf("failed to create cipher: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down...")
		cancel()
	}()

	server := relay.NewServer(cipher)
	if err := server.Run(ctx, *listen); err != nil && err != context.Canceled {
		log.Fatalf("server error: %v", err)
	}
}
