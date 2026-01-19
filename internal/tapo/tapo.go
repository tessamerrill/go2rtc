package tapo

import (
	"fmt"
	"net"
	"net/url"

	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/kasa"
	"github.com/AlexxIT/go2rtc/pkg/tapo"
)

func Init() {
	streams.HandleFunc("kasa", func(source string) (core.Producer, error) {
		return kasa.Dial(source)
	})

	streams.HandleFunc("tapo", func(source string) (core.Producer, error) {
		return tapo.Dial(source)
	})

	streams.HandleFunc("vigi", func(source string) (core.Producer, error) {
		return tapo.Dial(source)
	})

	// Consumer handler for Kasa two-way audio
	streams.HandleConsumerFunc("kasa-speaker", func(source string) (core.Consumer, func(), error) {
		// Parse URL: kasa-speaker://username:password@camera-ip
		u, err := url.Parse(source)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid kasa-speaker URL: %w", err)
		}

		// Extract credentials
		username := u.User.Username()
		password, _ := u.User.Password()

		if username == "" || password == "" {
			return nil, nil, fmt.Errorf("kasa-speaker: username and password required")
		}

		// Extract camera IP (hostname without port)
		// Use net.SplitHostPort to properly handle IPv6 addresses
		cameraIP := u.Hostname()
		if cameraIP == "" {
			return nil, nil, fmt.Errorf("kasa-speaker: camera IP required")
		}

		// If there's a port in the URL, remove it (we use hardcoded ports)
		// SplitHostPort already handles IPv6 properly
		if host, _, err := net.SplitHostPort(u.Host); err == nil {
			cameraIP = host
		}

		cons := kasa.NewConsumer(cameraIP, username, password)

		// Run function to start the consumer and keep it alive
		run := func() {
			if err := cons.Start(); err != nil {
				// Error starting - consumer will be removed by the stream
				return
			}
			// Wait until stopped
			<-cons.Done()
		}

		return cons, run, nil
	})
}
