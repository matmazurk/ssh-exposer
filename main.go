package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"

	"github.com/matmazurk/ssh-exposer/exposer"
)

var authToken string

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	wg := &sync.WaitGroup{}
	go func() {
		if err := exposer.Run(ctx, wg, authToken, func(tunAddr string) error { return nil }); err != nil {
			log.Fatal(err)
		}
	}()

	<-c
	fmt.Println()
	log.Println("cancelling...")
	cancel()
	wg.Wait()
	log.Println("done")
}
