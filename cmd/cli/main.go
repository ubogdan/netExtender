package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ubogdan/netExtender"
)

func main() {
	vpn, err := netExtender.New("vpn.local:4433")
	if err != nil {
		log.Printf("New %s", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {

		select {
		case <-sigChan:
			log.Printf("Terminate ...")

			vpn.Disconnect()
		case <-time.After(30 * time.Second):
			//log.Printf("Disconnecting...")
			//vpn.Disconnect()
		}
		time.Sleep(1 * time.Second)
	}()

	err = vpn.Connect("ubogdan", "xxxxxx", "Local")
	if err != nil {
		log.Printf("Connect %s", err)
	}
}
