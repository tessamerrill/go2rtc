package kasa

// Consumer implements Kasa camera two-way audio (backchannel) support.
//
// This allows sending audio TO Kasa cameras (TP-Link KC200, KC401, KC420WS, KD110, etc)
// using the LINKIE2 protocol for session management and HTTPS streaming for audio data.
//
// Usage in go2rtc configuration:
//
//	streams:
//	  kasa_camera:
//	    # Receive video and audio FROM camera
//	    - kasa://admin:password@192.168.1.123:19443/https/stream/mixed
//	    # Send audio TO camera (backchannel)
//	    - kasa-speaker://admin:password@192.168.1.123
//
// The consumer supports G.711 µ-law (PCMU) and A-law (PCMA) audio at 8000 Hz.
// Based on the reference implementation in tessamerrill/kasa-ptz-frigate.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/pion/rtp"
)

const (
	// Audio streaming port and endpoint
	DataInPort       = 18443
	SpeakerEndpoint  = "/https/speaker/audio/g711block"
	DefaultSessionID = "TwoWayAudio_1"

	// Audio packet timing (50 FPS = 20ms intervals)
	AudioPacketInterval = 20 * time.Millisecond
)

// Consumer implements two-way audio for Kasa cameras
type Consumer struct {
	core.Connection

	client    *http.Client
	url       string
	cameraIP  string
	username  string
	password  string
	sessionID string

	streaming bool
	stopChan  chan struct{}
	doneChan  chan struct{}
	stopOnce  sync.Once
	mu        sync.Mutex
}

// NewConsumer creates a new Kasa two-way audio consumer
func NewConsumer(cameraIP, username, password string) *Consumer {
	return &Consumer{
		Connection: core.Connection{
			ID:         core.NewID(),
			FormatName: "kasa",
			Protocol:   "https",
			RemoteAddr: cameraIP,
			Medias: []*core.Media{
				{
					Kind:      core.KindAudio,
					Direction: core.DirectionSendonly,
					Codecs: []*core.Codec{
						{
							Name:      core.CodecPCMU,
							ClockRate: 8000,
						},
						{
							Name:      core.CodecPCMA,
							ClockRate: 8000,
						},
					},
				},
			},
		},
		cameraIP:  cameraIP,
		username:  username,
		password:  password,
		sessionID: DefaultSessionID,
		url:       fmt.Sprintf("https://%s:%d%s", cameraIP, DataInPort, SpeakerEndpoint),
		stopChan:  make(chan struct{}),
		doneChan:  make(chan struct{}),
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // #nosec G402 - Required for Kasa cameras with self-signed certs
				},
				// Keep-alive for continuous streaming
				DisableKeepAlives:   false,
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// AddTrack adds an audio track for streaming to the camera
func (c *Consumer) AddTrack(media *core.Media, codec *core.Codec, track *core.Receiver) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sender := core.NewSender(media, codec)

	// Handle RTP packets and stream them to camera
	sender.Handler = func(packet *rtp.Packet) {
		if !c.streaming {
			return
		}

		// Send audio payload to camera
		// Individual packet errors are expected during streaming (e.g. temporary network issues)
		// and should not stop the entire stream. Errors are silently ignored to maintain
		// continuous audio flow. Monitor connection health via the Stop/Done lifecycle.
		if err := c.sendAudio(packet.Payload); err != nil {
			return
		}

		c.Send += len(packet.Payload)
	}

	sender.HandleRTP(track)
	c.Senders = append(c.Senders, sender)

	return nil
}

// Start begins the two-way audio session
func (c *Consumer) Start() error {
	// Prepare RTC session
	if err := c.prepareRTCSession(); err != nil {
		return fmt.Errorf("failed to prepare RTC session: %w", err)
	}

	// Set session status to connected
	if err := c.setRTCSessionStatus(true); err != nil {
		return fmt.Errorf("failed to set RTC session status: %w", err)
	}

	c.mu.Lock()
	c.streaming = true
	c.mu.Unlock()

	return nil
}

// Stop ends the two-way audio session
func (c *Consumer) Stop() error {
	c.mu.Lock()
	if !c.streaming {
		c.mu.Unlock()
		return nil
	}
	c.streaming = false
	c.mu.Unlock()

	// Use sync.Once to ensure channels are only closed once
	c.stopOnce.Do(func() {
		close(c.stopChan)
		// Disconnect RTC session
		_ = c.setRTCSessionStatus(false)
		close(c.doneChan)
	})

	return c.Connection.Stop()
}

// Done returns a channel that blocks until the consumer is stopped
func (c *Consumer) Done() <-chan struct{} {
	return c.doneChan
}

// prepareRTCSession prepares the RTC session on the camera
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/onvif_server.py#L402-423
func (c *Consumer) prepareRTCSession() error {
	command := map[string]interface{}{
		"rtp": map[string]interface{}{
			"set_prepare_rtc_session": map[string]interface{}{
				"sessionId": c.sessionID,
			},
		},
	}

	response, err := sendLinkieCommand(c.cameraIP, command, c.username, c.password)
	if err != nil {
		return err
	}

	// Parse response to check for errors
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return fmt.Errorf("failed to parse prepare session response: %w", err)
	}

	// Check for error_code in response
	if rtp, ok := result["rtp"].(map[string]interface{}); ok {
		if prepareResp, ok := rtp["set_prepare_rtc_session"].(map[string]interface{}); ok {
			if errCode, ok := prepareResp["error_code"].(float64); ok && errCode != 0 {
				return fmt.Errorf("prepare session failed with error code: %v", errCode)
			}
		}
	}

	return nil
}

// setRTCSessionStatus sets the RTC session connection status
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/onvif_server.py#L425-450
func (c *Consumer) setRTCSessionStatus(connected bool) error {
	command := map[string]interface{}{
		"rtp": map[string]interface{}{
			"set_rtc_session_status": map[string]interface{}{
				"sessionId": c.sessionID,
				"connected": connected,
			},
		},
	}

	response, err := sendLinkieCommand(c.cameraIP, command, c.username, c.password)
	if err != nil {
		return err
	}

	// Parse response to check for errors
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return fmt.Errorf("failed to parse session status response: %w", err)
	}

	// Check for error_code in response
	if rtp, ok := result["rtp"].(map[string]interface{}); ok {
		if statusResp, ok := rtp["set_rtc_session_status"].(map[string]interface{}); ok {
			if errCode, ok := statusResp["error_code"].(float64); ok && errCode != 0 {
				return fmt.Errorf("set session status failed with error code: %v", errCode)
			}
		}
	}

	return nil
}

// sendAudio sends audio data to the camera speaker
// Based on: https://github.com/tessamerrill/kasa-ptz-frigate/blob/main/onvif_server.py#L451-534
func (c *Consumer) sendAudio(data []byte) error {
	req, err := http.NewRequest("POST", c.url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	// Set headers to match Kasa app
	// Note: Content-Type is 'audio/g711' which is the generic type for both PCMU and PCMA
	// The camera accepts both µ-law and A-law under this Content-Type as they are both G.711 variants
	req.Header.Set("Content-Type", "audio/g711")
	req.Header.Set("User-Agent", "Kasa_Android/3.4.9.1119")
	req.Header.Set("Connection", "keep-alive")
	req.ContentLength = int64(len(data))

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("audio send failed: %s", resp.Status)
	}

	return nil
}
