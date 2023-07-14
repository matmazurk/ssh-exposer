package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

var authToken string

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	wg := &sync.WaitGroup{}
	go run(ctx, wg)

	<-c
	fmt.Println()
	log.Println("cancelling...")
	cancel()
	wg.Wait()
	log.Println("done")
}

func run(ctx context.Context, wg *sync.WaitGroup) {
	tun, err := getTunnel(ctx)
	if err != nil {
		log.Fatal(err)
	}

	if publishTunnelAddress(tun.Addr().String()) != nil {
		log.Fatal(err)
	}

	log.Printf("listening on %s", tun.Addr())

	runListener(ctx, tun, wg)
}

func publishTunnelAddress(addr string) error {
	// resp, err := resty.New().R().SetBody(tun.URL()).Post("https://mazoft.pl/wies")
	// if err != nil {
	// 	log.Fatal(err.Error())
	// }
	// log.Printf("tunnel registered with %d response code\n", resp.StatusCode())
	return nil
}

func getTunnel(ctx context.Context) (net.Listener, error) {
	isUnrecoverable := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "ERR_NGROK_108")
	}
	const retries = 100

	var tun ngrok.Tunnel
	var err error
	for i := 0; i < retries; i++ {
		tun, err = ngrok.Listen(ctx,
			config.TCPEndpoint(),
			ngrok.WithAuthtoken(authToken),
			ngrok.WithDisconnectHandler(func(ctx context.Context, sess ngrok.Session, err error) {
				if isUnrecoverable(err) {
					sess.Close()
				}
			}),
		)
		if err != nil {
			if isUnrecoverable(err) {
				return nil, err
			}
			log.Printf("retry no:%d. ngrok listen error:%s", i+1, err.Error())
		}
		if err == nil {
			return tun, nil
		}
		time.Sleep(time.Second * 5)
	}

	return tun, nil
}

func runListener(ctx context.Context, l net.Listener, wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	for {
		conn, err := l.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "Tunnel closed") {
				return
			}
			log.Println(err)
			return
		}

		log.Println("forwarding connection from:", conn.RemoteAddr())
		wg.Add(1)
		go func() {
			defer wg.Done()

			err := handleConn(ctx, conn)
			if err != nil {
				log.Println(err)
			}

			log.Printf("forwarding from %s finished\n", conn.RemoteAddr())
		}()
	}
}

func handleConn(ctx context.Context, conn net.Conn) error {
	sshConn, err := net.Dial("tcp", "localhost:2222")
	if err != nil {
		return err
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(conn, sshConn)
	}()

	clientClosed := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(sshConn, conn)
		close(clientClosed)
	}()

	select {
	case <-ctx.Done():
	case <-clientClosed:
	}

	sshConn.Close()
	wg.Wait()

	return nil
}
