package main

import (
	"context"
	"fmt"
	ncw "github.com/Fast-IQ/notify-change-window"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGABRT)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			<-quit
			cancel()
			fmt.Println("Exit. Wait close.")
			//other close operation
		}
	}()

	windMsg := make(chan ncw.MessageCAW, 3)

	ncw.Subscribe(ctx, windMsg)

	for {
		select {
		case msg := <-windMsg:
			fmt.Println("windMsg:", msg.WindowName)
		case <-ctx.Done():
			goto End
		case <-time.After(time.Second * 10):
			goto End
		}
	}

End:
	fmt.Println("Exit")

}
