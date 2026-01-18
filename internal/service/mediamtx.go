package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	mediamtx "alpineworks.io/gomediamtx"
)

// MediaMTXClient wraps the gomediamtx client with functional options.
type MediaMTXClient struct {
	client    *mediamtx.ClientWithResponses
	logger    *slog.Logger
	publicURL string
}

// MediaMTXOption is a functional option for configuring the MediaMTX client.
type MediaMTXOption func(*mediaAPIOptions)

type mediaAPIOptions struct {
	httpClient *http.Client
	logger     *slog.Logger
	timeout    time.Duration
}

func defaultMediaMTXOptions() *mediaAPIOptions {
	return &mediaAPIOptions{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     slog.Default(),
		timeout:    10 * time.Second,
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(c *http.Client) MediaMTXOption {
	return func(o *mediaAPIOptions) {
		o.httpClient = c
	}
}

// WithMediaMTXLogger sets the logger.
func WithMediaMTXLogger(l *slog.Logger) MediaMTXOption {
	return func(o *mediaAPIOptions) {
		o.logger = l
	}
}

// WithTimeout sets the request timeout.
func WithTimeout(d time.Duration) MediaMTXOption {
	return func(o *mediaAPIOptions) {
		o.timeout = d
	}
}

// NewMediaMTXClient creates a new MediaMTX client.
func NewMediaMTXClient(apiURL, publicURL string, opts ...MediaMTXOption) (*MediaMTXClient, error) {
	cfg := defaultMediaMTXOptions()
	for _, opt := range opts {
		opt(cfg)
	}

	client, err := mediamtx.NewClientWithResponses(apiURL, mediamtx.WithHTTPClient(cfg.httpClient))
	if err != nil {
		return nil, fmt.Errorf("failed to create MediaMTX client: %w", err)
	}

	return &MediaMTXClient{
		client:    client,
		logger:    cfg.logger,
		publicURL: publicURL,
	}, nil
}

// KickPath kicks all connections from a specific path.
// MediaMTX doesn't have a direct "kick by path" API, so we need to:
// 1. List RTMP connections and kick any matching the path
// 2. List RTSP sessions and kick any matching the path
func (c *MediaMTXClient) KickPath(ctx context.Context, path string) error {
	// Try to kick RTMP connections for this path
	rtmpResp, err := c.client.RtmpConnsListWithResponse(ctx, nil)
	if err != nil {
		c.logger.Warn("failed to list RTMP connections",
			slog.String("error", err.Error()),
		)
	} else if rtmpResp.JSON200 != nil && rtmpResp.JSON200.Items != nil {
		for _, conn := range *rtmpResp.JSON200.Items {
			if conn.Path != nil && *conn.Path == path && conn.Id != nil {
				_, kickErr := c.client.RtmpConnsKickWithResponse(ctx, *conn.Id)
				if kickErr != nil {
					c.logger.Warn("failed to kick RTMP connection",
						slog.String("error", kickErr.Error()),
						slog.String("connection_id", *conn.Id),
					)
				} else {
					c.logger.Info("kicked RTMP connection",
						slog.String("connection_id", *conn.Id),
						slog.String("path", path),
					)
				}
			}
		}
	}

	// Try to kick RTSP sessions for this path
	rtspResp, err := c.client.RtspSessionsListWithResponse(ctx, nil)
	if err != nil {
		c.logger.Warn("failed to list RTSP sessions",
			slog.String("error", err.Error()),
		)
	} else if rtspResp.JSON200 != nil && rtspResp.JSON200.Items != nil {
		for _, session := range *rtspResp.JSON200.Items {
			if session.Path != nil && *session.Path == path && session.Id != nil {
				_, kickErr := c.client.RtspSessionsKickWithResponse(ctx, *session.Id)
				if kickErr != nil {
					c.logger.Warn("failed to kick RTSP session",
						slog.String("error", kickErr.Error()),
						slog.String("session_id", *session.Id),
					)
				} else {
					c.logger.Info("kicked RTSP session",
						slog.String("session_id", *session.Id),
						slog.String("path", path),
					)
				}
			}
		}
	}

	return nil
}

// GetHLSURL returns the HLS URL for a stream path.
func (c *MediaMTXClient) GetHLSURL(path string) string {
	return fmt.Sprintf("%s/%s/index.m3u8", c.publicURL, path)
}

// GetWebRTCURL returns the WebRTC WHEP URL for a stream path.
func (c *MediaMTXClient) GetWebRTCURL(path string) string {
	return fmt.Sprintf("%s/%s/whep", c.publicURL, path)
}

// GetRTMPURL returns the RTMP URL for a stream path.
func (c *MediaMTXClient) GetRTMPURL(path string) string {
	return fmt.Sprintf("rtmp://%s/%s", c.publicURL, path)
}
