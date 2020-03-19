package netExtender

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

const (
	mtu = 1280
)

func (s *session) dialTunnel() error {
	conn, err := tls.Dial("tcp", s.hostname, s.tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	req := &http.Request{
		Method:     http.MethodConnect,
		ProtoMajor: 1,
		ProtoMinor: 0,
		Close:      true,
		URL:        &url.URL{Opaque: "localhost:0"},
		Host:       "localhost:0", // This is weird
		Header: http.Header{
			hdrUserAgent:           []string{hdrUserAgentName},
			"X-SSLVPN-PROTOCOL":    []string{"2.0"},
			"X-SSLVPN-SERVICE":     []string{"NETEXTENDER"},
			hdrProxyAuth:           []string{s.sessionID},
			"X-NX-Client-Platform": []string{"Linux"},
			"Connection-Medium":    []string{"MacOS"},
			"X-NE-PROTOCOL":        []string{"2.0"},
			"Frame-Encode":         []string{"off"},
		},
	}

	err = req.Write(conn)
	if err != nil {
		return fmt.Errorf("req.Write %s", err)
	}

	// More about params https://ppp.samba.org/pppd.html
	params := []string{"call", "softvpn", "mtu", "1280", "mru", "1280", "debug", "debug", "logfd", "2"}

	cmd := exec.Command("/usr/sbin/pppd", params...)
	cmd.Stderr = os.Stderr
	pppd, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty.Start %s", err)
	}

	errChan := make(chan error)

	go func() {
		rcvBuff := make([]byte, 8192)
		var length uint32

		for {
			//conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			rn, err := conn.Read(rcvBuff)
			if err != nil {
				errChan <- fmt.Errorf("conn.Read %s", err)
				return
			}

			length = binary.BigEndian.Uint32(rcvBuff)
			if uint32(rn) < 4+length {
				log.Printf("Received bigger bufffer %d,%d", rn, length)
				log.Printf("%s", rcvBuff[:rn])
				//continue
			}

			_, err = pppd.Write(rcvBuff[4 : 4+length])
			if err != nil {
				errChan <- fmt.Errorf("pipe.Write %s", err)
				return
			}

		}
	}()
	go func() {
		writeBuff := make([]byte, 8192)
		for {
			//pppd.SetReadDeadline(time.Now().Add(30 * time.Second))
			wn, err := pppd.Read(writeBuff)
			if err != nil {
				errChan <- fmt.Errorf("pipe.Read %s", err)
				return
			}

			for wn > 0 {
				size := wn

				if size > mtu {
					size = mtu
					log.Printf("Received size %d", size)
				}

				// Encode buffer
				buffer := make([]byte, 4)
				binary.BigEndian.PutUint32(buffer, uint32(size))
				buffer = append(buffer, writeBuff[:size]...)

				// Cut the buffer
				writeBuff = append(writeBuff[:size], writeBuff[wn:]...)
				wn -= size

				_, err = conn.Write(buffer)
				if err != nil {
					errChan <- fmt.Errorf("conn.Write %s", err)
					return
				}
			}
		}
	}()

	for {
		select {
		case <-s.shutdown:
			return nil
		case err := <-errChan:
			return err
		}
	}
	return nil
}
