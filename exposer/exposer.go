package exposer

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

var SshdPort = 22

type Publish func(tunAddr string) error

func Run(ctx context.Context, wg *sync.WaitGroup, authToken string, publish Publish) error {
	tun, err := getTunnel(ctx, authToken)
	if err != nil {
		return err
	}

	if err := publish(tun.Addr().String()); err != nil {
		return err
	}

	log.Printf("listening on %s", tun.Addr())

	listen(ctx, tun, wg)
	return nil
}

func getTunnel(ctx context.Context, authToken string) (net.Listener, error) {
	isUnrecoverable := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "ERR_NGROK_108")
	}

	const retries = 100
	for i := 0; i < retries; i++ {
		tun, err := ngrok.Listen(ctx,
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

	return nil, errors.Errorf("couldn't create tunnel after %d retries", retries)
}

func listen(ctx context.Context, l net.Listener, wg *sync.WaitGroup) error {
	wg.Add(1)
	defer wg.Done()

	for {
		conn, err := l.Accept()
		if err != nil {
			if strings.Contains(err.Error(), "Tunnel closed") {
				return nil
			}
			return err
		}

		log.Println("forwarding connection from:", conn.RemoteAddr())
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := pipeIncomingConnection(ctx, conn); err != nil {
				log.Println(err)
			}

			log.Printf("forwarding from %s finished\n", conn.RemoteAddr())
		}()
	}
}

func pipeIncomingConnection(ctx context.Context, conn net.Conn) error {
	sshConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", SshdPort))
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
