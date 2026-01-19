package tapo

import (
	"fmt"
	"net/url"
	"strings"

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
		cameraIP := u.Hostname()
		if cameraIP == "" {
			return nil, nil, fmt.Errorf("kasa-speaker: camera IP required")
		}

		// Remove port if specified (we use hardcoded ports)
		cameraIP = strings.Split(cameraIP, ":")[0]

		cons := kasa.NewConsumer(cameraIP, username, password)

		// Closer function to stop the consumer
		closer := func() {
			_ = cons.Stop()
		}

		return cons, closer, nil
	})
}
