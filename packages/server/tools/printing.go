package tools

// printing — 3D printing pipeline: Blender generation + Bambu A1 printer control.
//
// Blender:
//   Runs Python scripts headlessly via `blender --background --python <script>`.
//   Blender must be installed and available in PATH.
//
// Bambu A1:
//   Communicates over MQTT (TLS, port 8883) using the Bambu LAN protocol.
//   Requires BAMBU_IP, BAMBU_ACCESS_CODE, and BAMBU_SERIAL env vars.
//   TLS certificate verification is disabled (Bambu printers use self-signed certs).
//
// Tools:
//   blender_generate — run a Blender Python script headlessly to produce a .blend or .stl
//   bambu_status     — subscribe to the printer's MQTT report topic and return current state
//   bambu_print      — publish a print job command to the printer's request topic
//   bambu_stop       — publish a stop command to cancel the current print job

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/caboose-mcp/server/config"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterPrinting(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("blender_generate",
		mcp.WithDescription("Run a Blender Python script headlessly to generate a 3D file."),
		mcp.WithString("script", mcp.Required(), mcp.Description("Python script code to run in Blender")),
		mcp.WithString("output_path", mcp.Required(), mcp.Description("Output file path (.blend or .stl)")),
	), blenderGenerateHandler(cfg))

	s.AddTool(mcp.NewTool("bambu_status",
		mcp.WithDescription("Get current status from a Bambu A1 printer over MQTT."),
	), bambuStatusHandler(cfg))

	s.AddTool(mcp.NewTool("bambu_print",
		mcp.WithDescription("Start a print job on the Bambu A1 printer."),
		mcp.WithString("file_path", mcp.Required(), mcp.Description("Path to .3mf or .gcode file")),
		mcp.WithNumber("bed_temp", mcp.Description("Bed temperature override")),
		mcp.WithNumber("nozzle_temp", mcp.Description("Nozzle temperature override")),
	), bambuPrintHandler(cfg))

	s.AddTool(mcp.NewTool("bambu_stop",
		mcp.WithDescription("Stop the current print job on the Bambu A1 printer."),
	), bambuStopHandler(cfg))
}

func blenderGenerateHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		script, err := req.RequireString("script")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		outputPath, err := req.RequireString("output_path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		tmpFile, err := os.CreateTemp("", "blender-script-*.py")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("temp file error: %v", err)), nil
		}
		defer os.Remove(tmpFile.Name())
		if _, err := tmpFile.WriteString(script); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("write script error: %v", err)), nil
		}
		tmpFile.Close()

		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("mkdir error: %v", err)), nil
		}

		out, err := exec.Command("blender", "--background", "--python", tmpFile.Name()).CombinedOutput()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("blender error: %v\n%s", err, out)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("blender completed\n%s", out)), nil
	}
}

func bambuMQTTClient(cfg *config.Config) (mqtt.Client, error) {
	if cfg.BambuIP == "" || cfg.BambuAccessCode == "" || cfg.BambuSerial == "" {
		return nil, fmt.Errorf("`bambu_status`, `bambu_print`, and `bambu_stop` are not yet set up.\n\nTo configure them, set:\n  BAMBU_IP=<printer-ip>\n  BAMBU_ACCESS_CODE=<access-code>\n  BAMBU_SERIAL=<serial-number>")
	}
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tls://%s:8883", cfg.BambuIP))
	opts.SetClientID("caboose-mcp")
	opts.SetUsername("bblp")
	opts.SetPassword(cfg.BambuAccessCode)

	tlsConfig, err := bambuTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	opts.SetTLSConfig(tlsConfig)
	opts.SetConnectTimeout(10 * time.Second)
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	return client, nil
}

// bambuTLSConfig returns a TLS config for Bambu MQTT connections.
//
// If a custom CA cert exists at ~/.claude/bambu-ca.crt, it is used for
// certificate pinning: the server certificate is verified against that CA
// (chain validation without hostname/IP SAN check, since Bambu printers
// present self-signed certs served by IP address).
//
// If no CA cert file is found (os.IsNotExist), insecure mode is allowed
// only for private/loopback addresses (LAN-local use). Any other error
// reading the CA cert file is returned immediately.
func bambuTLSConfig(cfg *config.Config) (*tls.Config, error) {
	caCertPath := filepath.Join(cfg.ClaudeDir, "bambu-ca.crt")
	certData, err := os.ReadFile(caCertPath)
	if err == nil {
		// Custom CA cert found: pin to it and verify the chain without hostname check.
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(certData) {
			return nil, fmt.Errorf("failed to parse Bambu CA certificate at %s", caCertPath)
		}
		return &tls.Config{
			// InsecureSkipVerify disables Go's built-in hostname/SAN check;
			// VerifyPeerCertificate below performs chain validation against our pinned CA.
			InsecureSkipVerify:    true, //nolint:gosec
			MinVersion:            tls.VersionTLS12,
			VerifyPeerCertificate: verifyAgainstPool(caCertPool),
		}, nil
	}

	if !os.IsNotExist(err) {
		// Unexpected error (permission denied, I/O error, etc.) — fail explicitly.
		return nil, fmt.Errorf("reading Bambu CA certificate at %s: %w", caCertPath, err)
	}

	// No CA cert file present. Only allow insecure TLS for private/loopback addresses.
	// Note: BAMBU_IP must be a numeric IP address (not a hostname); net.ParseIP does not
	// resolve DNS names.
	ip := net.ParseIP(cfg.BambuIP)
	if ip == nil || (!ip.IsPrivate() && !ip.IsLoopback()) {
		return nil, fmt.Errorf(
			"BAMBU_IP %q must be a private or loopback IP address (not a hostname); provide a CA certificate at %s for non-LAN connections",
			cfg.BambuIP, caCertPath,
		)
	}
	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // acceptable for local LAN use only
		MinVersion:         tls.VersionTLS12,
	}, nil
}

// verifyAgainstPool returns a VerifyPeerCertificate function that validates
// the server's certificate chain against pool without checking hostname/IP SANs.
func verifyAgainstPool(pool *x509.CertPool) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no certificates presented by server")
		}
		certs := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, err := x509.ParseCertificate(asn1Data)
			if err != nil {
				return fmt.Errorf("parsing server certificate: %w", err)
			}
			certs[i] = cert
		}
		intermediates := x509.NewCertPool()
		for _, cert := range certs[1:] {
			intermediates.AddCert(cert)
		}
		if _, err := certs[0].Verify(x509.VerifyOptions{
			Roots:         pool,
			Intermediates: intermediates,
		}); err != nil {
			return fmt.Errorf("certificate chain validation failed: %w", err)
		}
		return nil
	}
}

func bambuStatusHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := bambuMQTTClient(cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer client.Disconnect(250)

		topic := fmt.Sprintf("device/%s/report", cfg.BambuSerial)
		var (
			mu      sync.Mutex
			payload string
			done    = make(chan struct{})
		)

		client.Subscribe(topic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			mu.Lock()
			defer mu.Unlock()
			payload = string(msg.Payload())
			select {
			case <-done:
			default:
				close(done)
			}
		})

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			return mcp.NewToolResultError("timeout waiting for printer status"), nil
		}
		return mcp.NewToolResultText(payload), nil
	}
}

func bambuPrintHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := req.RequireString("file_path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		bedTemp := req.GetInt("bed_temp", cfg.BambuBedTemp)
		nozzleTemp := req.GetInt("nozzle_temp", cfg.BambuNozzleTemp)

		client, err := bambuMQTTClient(cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer client.Disconnect(250)

		printCmd := map[string]any{
			"print": map[string]any{
				"sequence_id":    "1",
				"command":        "project_file",
				"param":          "Metadata/plate_1.gcode",
				"url":            fmt.Sprintf("ftp://%s/%s", cfg.BambuIP, filepath.Base(filePath)),
				"bed_type":       "auto",
				"timelapse":      false,
				"bed_leveling":   true,
				"flow_cali":      false,
				"vibration_cali": false,
				"layer_inspect":  false,
				"use_ams":        false,
				"bed_temp":       bedTemp,
				"nozzle_temp":    nozzleTemp,
			},
		}

		payload, _ := json.Marshal(printCmd)
		topic := fmt.Sprintf("device/%s/request", cfg.BambuSerial)
		token := client.Publish(topic, 0, false, payload)
		token.Wait()
		if token.Error() != nil {
			return mcp.NewToolResultError(fmt.Sprintf("publish error: %v", token.Error())), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("print job sent for %s (bed=%d°C, nozzle=%d°C)", filepath.Base(filePath), bedTemp, nozzleTemp)), nil
	}
}

func bambuStopHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		client, err := bambuMQTTClient(cfg)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		defer client.Disconnect(250)

		stopCmd := map[string]any{
			"print": map[string]any{
				"sequence_id": "1",
				"command":     "stop",
				"param":       "",
			},
		}
		payload, _ := json.Marshal(stopCmd)
		topic := fmt.Sprintf("device/%s/request", cfg.BambuSerial)
		token := client.Publish(topic, 0, false, payload)
		token.Wait()
		if token.Error() != nil {
			return mcp.NewToolResultError(fmt.Sprintf("publish error: %v", token.Error())), nil
		}
		return mcp.NewToolResultText("stop command sent"), nil
	}
}
