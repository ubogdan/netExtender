package netExtender

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type session struct {
	hostname    string
	routes      []string
	nameservers []string
	sessionID   string
	client      *http.Client
	tlsConfig   *tls.Config
	shutdown    chan struct{}
}

type Session interface {
	Connect(username, password, domain string) error
	Disconnect() error
}

const (
	hdrUserAgent       = "User-Agent"
	hdrContentType     = "Content-Type"
	hdrContentTypeForm = "application/x-www-form-urlencoded"
	hdrUserAgentName   = "SonicWALL NetExtender for Linux 8.6.801"
	hdrProxyAuth       = "Proxy-Authorization"
	hdrNetExtenderPDA  = "X-Ne-Pda"
)

func New(hostname string) (Session, error) {
	cookieJar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	return &session{
		hostname: hostname,
		client: &http.Client{
			Jar: cookieJar,
		},
		shutdown: make(chan struct{}),
	}, nil
}

func (s *session) Connect(username, password, domain string) error {
	log.Print("Logging in...")

	err := s.login(username, password, domain)
	if err != nil {
		return fmt.Errorf("login %s", err)
	}
	defer func() {
		err = s.logout()
		if err != nil {
			log.Printf("logout %s", err)
		}
	}()

	err = s.getEpcVersion()
	if err != nil {
		return fmt.Errorf("login %s", err)
	}

	log.Print("Starting session...")
	err = s.getSession()
	if err != nil {
		return fmt.Errorf("session %s", err)
	}

	time.Sleep(time.Second)

	log.Print("Dialing up tunnel...")
	err = s.dialTunnel()
	if err != nil {
		return fmt.Errorf("Dial %s", err)
	}

	return nil
}

func (s *session) Disconnect() error {
	s.shutdown <- struct{}{}
	return nil
}

func (s *session) login(username, password, domain string) error {
	data := url.Values{
		"username": []string{username},
		"password": []string{password},
		"domain":   []string{domain},
		"login":    []string{"true"},
	}

	request, err := s.newRequest("POST", "cgi-bin/userLogin", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	request.Header.Add(hdrContentType, hdrContentTypeForm)
	request.Header.Add(hdrNetExtenderPDA, "true")

	res, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	fail, err := strconv.ParseInt(res.Header.Get("X-Ne-Tfresult"), 10, 64)
	if err != nil {
		return fmt.Errorf("Unexpected X-Ne-Tfresult value")
	}

	if fail > 0 {
		msg := res.Header.Get("X-Ne-Message")
		if len(msg) > 0 {
			return errors.New(msg)
		}
		return errors.New("Login failed - User login denied - Account already in use and uniqueness enable.")
	}

	return nil
}

func (s *session) getEpcVersion() error {
	request, err := s.newRequest("GET", "cgi-bin/sslvpnclient?epcversionquery=nxx", nil)
	if err != nil {
		return err
	}

	res, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return nil
}

func (s *session) getSession() error {
	request, err := s.newRequest("GET", "cgi-bin/sslvpnclient?launchplatform=mac&neProto=3&supportipv6=yes", nil)
	if err != nil {
		return err
	}
	request.Header.Add(hdrNetExtenderPDA, "true")

	res, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "=") {
			line = strings.Trim(line, ";")
			kv := strings.Split(line, "=")
			log.Print(line)
			name := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			switch name {
			case "SessionId":

			case "Route":
				s.routes = append(s.routes, value)

			case "dns1", "dns2":
				s.nameservers = append(s.nameservers, value)
			}
		}
	}
	for _, cookie := range res.Cookies() {
		if cookie.Name == "swap" {
			s.sessionID = cookie.Value
		}
	}
	return nil
}

func (s *session) logout() error {
	request, err := s.newRequest("POST", "cgi-bin/userLogout", strings.NewReader(" "))
	if err != nil {
		return err
	}
	request.Header.Add(hdrContentType, hdrContentTypeForm)
	request.Header.Add(hdrNetExtenderPDA, "true")

	res, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return nil
}

func (s *session) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequest(method, fmt.Sprintf("https://%s/%s", s.hostname, path), body)
	if err != nil {
		return nil, err
	}
	request.Header.Add(hdrUserAgent, hdrUserAgentName)
	return request, nil
}
